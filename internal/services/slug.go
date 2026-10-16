package services

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "rs"
	}
	return s
}

// UniqueSlug pastiin slug unik di tabel & kolom tertentu, nambahin -2, -3, dst
// kalau bentrok. excludeID = 0 berarti tidak ada record yang dikecualikan
// (dipakai saat create; saat update isi dengan ID record-nya sendiri).
func UniqueSlug(db *gorm.DB, table, name string, excludeID uint) string {
	base := Slugify(name)
	slug := base

	for i := 2; ; i++ {
		var count int64
		q := db.Table(table).Where("slug = ?", slug)
		if excludeID != 0 {
			q = q.Where("id <> ?", excludeID)
		}
		q.Count(&count)
		if count == 0 {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}
