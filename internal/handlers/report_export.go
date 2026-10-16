package handlers

import (
	"fmt"
	"math"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

var monthNames = []string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli",
	"Agustus", "September", "Oktober", "November", "Desember"}

// periodLabel: "29 Sep 2026" (harian) or "September 2026" (bulanan).
func periodLabel(period, key string) string {
	if period == "monthly" {
		t, err := time.Parse("2006-01", key)
		if err != nil {
			return key
		}
		return fmt.Sprintf("%s %d", monthNames[t.Month()-1], t.Year())
	}
	t, err := time.Parse("2006-01-02", key)
	if err != nil {
		return key
	}
	return fmt.Sprintf("%02d %s %d", t.Day(), monthNames[t.Month()-1][:3], t.Year())
}

func minutesOrNil(v *float64) any {
	if v == nil {
		return nil
	}
	return math.Round(*v)
}

// RecapExport — GET /api/reports/recap/export (same query as Recap) → .xlsx
func (h *ReportHandler) RecapExport(c *fiber.Ctx) error {
	res, err := h.recap(c)
	if err != nil {
		return err
	}

	scope := ""
	if org, err := partnerRS(h.DB); err == nil {
		scope = org.Name
	}
	title := "Rekap Harian"
	rangeText := periodLabel("monthly", res.From[:7])
	if res.Period == "monthly" {
		title = "Rekap Bulanan"
		rangeText = "Tahun " + res.From[:4]
	}

	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Rekap"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	f.SetCellValue(sheet, "A1", "MedSend — "+title)
	f.SetCellValue(sheet, "A2", fmt.Sprintf("%s · %s · berdasarkan tanggal order masuk · diekspor %s",
		scope, rangeText, time.Now().Format("02 Jan 2006 15:04")))

	headers := []string{"Periode", "Masuk", "Selesai", "Dibatalkan", "Diproses"}
	for _, s := range res.Services {
		headers = append(headers, s.Name)
	}
	headers = append(headers, "Rata-rata siap diambil (menit)", "Rata-rata selesai (menit)")
	withRevenue := res.Total.Revenue != nil // Admin & Finance only
	if withRevenue {
		headers = append(headers, "Pendapatan")
	}
	for i, t := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 4)
		f.SetCellValue(sheet, cell, t)
	}

	values := func(r recapRow) []any {
		v := []any{r.Total, r.Done, r.Cancelled, r.InProgress}
		for _, s := range res.Services {
			v = append(v, r.ByService[s.ID])
		}
		v = append(v, minutesOrNil(r.AvgReadyMin), minutesOrNil(r.AvgDoneMin))
		if withRevenue {
			v = append(v, *r.Revenue)
		}
		return v
	}
	row := 5
	for _, r := range res.Rows {
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), periodLabel(res.Period, r.Period))
		for i, v := range values(r) {
			cell, _ := excelize.CoordinatesToCellName(i+2, row)
			f.SetCellValue(sheet, cell, v)
		}
		row++
	}
	f.SetCellValue(sheet, fmt.Sprintf("A%d", row), "Total")
	for i, v := range values(res.Total) {
		cell, _ := excelize.CoordinatesToCellName(i+2, row)
		f.SetCellValue(sheet, cell, v)
	}

	last, _ := excelize.CoordinatesToCellName(len(headers), 4)
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	titleStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14, Color: "18186C"}})
	headStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "18186C"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F0F0FB"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	moneyFmt := `"Rp" #,##0`
	moneyStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &moneyFmt})
	totalStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true},
		Border: []excelize.Border{{Type: "top", Color: "18186C", Style: 1}}})
	totalMoney, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, CustomNumFmt: &moneyFmt,
		Border: []excelize.Border{{Type: "top", Color: "18186C", Style: 1}}})

	f.SetCellStyle(sheet, "A1", "A1", titleStyle)
	f.SetCellStyle(sheet, "A4", last, headStyle)
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("%s%d", lastCol, row), totalStyle)
	if withRevenue {
		f.SetCellStyle(sheet, fmt.Sprintf("%s5", lastCol), fmt.Sprintf("%s%d", lastCol, row-1), moneyStyle)
		f.SetCellStyle(sheet, fmt.Sprintf("%s%d", lastCol, row), fmt.Sprintf("%s%d", lastCol, row), totalMoney)
	}
	f.SetRowHeight(sheet, 4, 32)
	f.SetColWidth(sheet, "A", "A", 18)
	f.SetColWidth(sheet, "B", lastCol, 13)
	f.SetColWidth(sheet, lastCol, lastCol, 16)
	f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5", ActivePane: "bottomLeft"})

	name := fmt.Sprintf("rekap-%s-%s.xlsx", map[string]string{"daily": "harian", "monthly": "bulanan"}[res.Period],
		map[string]string{"daily": res.From[:7], "monthly": res.From[:4]}[res.Period])
	c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s"`, name))
	buf, err := f.WriteToBuffer()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Send(buf.Bytes())
}
