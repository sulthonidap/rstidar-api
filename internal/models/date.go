package models

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// Date is a calendar day (SQL DATE) that goes to/from JSON as "YYYY-MM-DD",
// with no time or timezone attached.
type Date struct{ time.Time }

const dateLayout = "2006-01-02"

func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation(dateLayout, strings.TrimSpace(s), time.Local)
	return Date{t}, err
}

func (d Date) String() string { return d.Format(dateLayout) }

func (d Date) MarshalJSON() ([]byte, error) { return []byte(`"` + d.String() + `"`), nil }

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*d = Date{}
		return nil
	}
	v, err := ParseDate(s)
	if err != nil {
		return fmt.Errorf("tanggal harus YYYY-MM-DD")
	}
	*d = v
	return nil
}

func (d *Date) Scan(v any) error {
	switch t := v.(type) {
	case time.Time:
		y, m, day := t.Date()
		d.Time = time.Date(y, m, day, 0, 0, 0, 0, time.Local)
		return nil
	case []byte:
		p, err := ParseDate(string(t))
		*d = p
		return err
	case string:
		p, err := ParseDate(t)
		*d = p
		return err
	}
	return fmt.Errorf("tidak bisa membaca tanggal dari %T", v)
}

func (d Date) Value() (driver.Value, error) { return d.String(), nil }
