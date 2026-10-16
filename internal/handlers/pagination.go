package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
)

// pageParams is parsed from ?page=&per_page= (page is 1-based).
type pageParams struct {
	Page    int
	PerPage int
}

func parsePage(c *fiber.Ctx) pageParams {
	p := pageParams{Page: c.QueryInt("page", 1), PerPage: c.QueryInt("per_page", defaultPerPage)}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = defaultPerPage
	}
	if p.PerPage > maxPerPage {
		p.PerPage = maxPerPage
	}
	return p
}

func (p pageParams) scope(db *gorm.DB) *gorm.DB {
	return db.Offset((p.Page - 1) * p.PerPage).Limit(p.PerPage)
}

func (p pageParams) meta(total int64) fiber.Map {
	lastPage := (total + int64(p.PerPage) - 1) / int64(p.PerPage)
	if lastPage < 1 {
		lastPage = 1
	}
	return fiber.Map{
		"page":      p.Page,
		"per_page":  p.PerPage,
		"total":     total,
		"last_page": lastPage,
	}
}

// filterCreatedDate applies ?from=YYYY-MM-DD&to=YYYY-MM-DD to created_at, both
// inclusive whole days in local time (to=15 keeps rows at 15th 23:59).
func filterCreatedDate(c *fiber.Ctx, query *gorm.DB) (*gorm.DB, error) {
	return filterDate(c, query, "created_at")
}

// filterDate is filterCreatedDate for any date column or SQL expression.
func filterDate(c *fiber.Ctx, query *gorm.DB, column string) (*gorm.DB, error) {
	if from := c.Query("from"); from != "" {
		d, err := time.ParseInLocation("2006-01-02", from, time.Local)
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "format tanggal from harus YYYY-MM-DD")
		}
		query = query.Where(column+" >= ?", d)
	}
	if to := c.Query("to"); to != "" {
		d, err := time.ParseInLocation("2006-01-02", to, time.Local)
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "format tanggal to harus YYYY-MM-DD")
		}
		query = query.Where(column+" < ?", d.AddDate(0, 0, 1))
	}
	return query, nil
}

// likePattern turns ?search= into a LIKE pattern, escaping the LIKE wildcards.
func likePattern(search string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(search)) + "%"
}
