package handlers

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/privacy"
)

// maxExportRows keeps one export from loading an unbounded table into memory.
const maxExportRows = 20000

var paymentLabels = map[string]string{"unpaid": "Belum bayar", "paid": "Lunas", "refunded": "Refund"}

// Export — GET /api/transactions/export?search=&from=&to=
// File .xlsx berisi SEMUA transaksi yang cocok dengan filter (bukan cuma satu halaman).
func (h *TransactionHandler) Export(c *fiber.Ctx) error {
	query, err := h.filtered(c)
	if err != nil {
		return err
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if count > maxExportRows {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			fmt.Sprintf("terlalu banyak data (%d baris, maks %d). Persempit rentang tanggal.", count, maxExportRows))
	}

	var list []models.Transaction
	if err := query.Preload("Order").Preload("Organization").Preload("RecordedByUser").
		Order("created_at DESC, id DESC").Find(&list).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Transaksi"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	// Title + filter info, then the table from row 4.
	period := "Semua tanggal"
	if from, to := c.Query("from"), c.Query("to"); from != "" || to != "" {
		period = fmt.Sprintf("%s s/d %s", orDash(from), orDash(to))
	}
	f.SetCellValue(sheet, "A1", "Laporan Transaksi MedSend")
	f.SetCellValue(sheet, "A2", fmt.Sprintf("Periode: %s · Diekspor %s", period, time.Now().Format("02 Jan 2006 15:04")))

	headers := []string{"No. Order", "RS / Instansi", "Customer", "Nominal", "Pembayaran", "Tgl bayar", "Dibuat", "Dicatat oleh"}
	for i, title := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 4)
		f.SetCellValue(sheet, cell, title)
	}

	titleStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14, Color: "18186C"}})
	headStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "18186C"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F0F0FB"}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	moneyFmt := `"Rp" #,##0`
	moneyStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &moneyFmt})
	dateFmt := "dd mmm yyyy hh:mm"
	dateStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &dateFmt})
	totalStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, CustomNumFmt: &moneyFmt})
	totalLabelStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})

	f.SetCellStyle(sheet, "A1", "A1", titleStyle)
	f.SetCellStyle(sheet, "A4", "H4", headStyle)

	var total float64
	for i, t := range list {
		row := i + 5
		code, customer, org, recordedBy := "", "", "", ""
		if t.Order != nil {
			code, customer = t.Order.Code, privacy.Name(t.Order.CustomerName)
		}
		if t.Organization != nil {
			org = t.Organization.Name
		}
		if t.RecordedByUser != nil {
			recordedBy = t.RecordedByUser.Name
		}
		payment := paymentLabels[string(t.PaymentStatus)]
		if payment == "" {
			payment = string(t.PaymentStatus)
		}
		// Never paid and the order was cancelled: nothing is owed any more.
		if t.PaymentStatus == enums.PaymentStatusUnpaid && t.Order != nil && t.Order.Status == enums.OrderStatusCancelled {
			payment = "Batal"
		}
		values := []any{code, org, customer, t.Amount, payment, nil, t.CreatedAt, recordedBy}
		if t.PaidAt != nil {
			values[5] = *t.PaidAt
		}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetSheetRow(sheet, cell, &values); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		// The total is money actually received: refunds and unpaid bills don't count.
		if t.PaymentStatus == enums.PaymentStatusPaid {
			total += t.Amount
		}
	}

	last := len(list) + 4
	if len(list) > 0 {
		f.SetCellStyle(sheet, "D5", fmt.Sprintf("D%d", last), moneyStyle)
		f.SetCellStyle(sheet, "F5", fmt.Sprintf("G%d", last), dateStyle)
	}
	totalRow := last + 1
	f.SetCellValue(sheet, fmt.Sprintf("C%d", totalRow), "Total lunas")
	f.SetCellValue(sheet, fmt.Sprintf("D%d", totalRow), total)
	f.SetCellStyle(sheet, fmt.Sprintf("C%d", totalRow), fmt.Sprintf("C%d", totalRow), totalLabelStyle)
	f.SetCellStyle(sheet, fmt.Sprintf("D%d", totalRow), fmt.Sprintf("D%d", totalRow), totalStyle)

	for col, width := range map[string]float64{"A": 20, "B": 24, "C": 24, "D": 14, "E": 13, "F": 19, "G": 19, "H": 20} {
		f.SetColWidth(sheet, col, col, width)
	}
	f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5", ActivePane: "bottomLeft"})
	if len(list) > 0 {
		f.AutoFilter(sheet, fmt.Sprintf("A4:H%d", last), nil)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	name := "transaksi"
	if from, to := c.Query("from"), c.Query("to"); from != "" || to != "" {
		name += "_" + orDash(from) + "_" + orDash(to)
	}
	c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s.xlsx"`, name))
	return c.Send(buf.Bytes())
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
