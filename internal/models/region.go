package models

// Region is one area from the Kemendagri region codes: level 1 provinsi
// ("33"), 2 kab/kota ("33.71"), 3 kecamatan ("33.71.01"), 4 kelurahan/desa
// ("33.71.01.1001"). A code starts with its parent's code.
type Region struct {
	Code       string  `gorm:"primaryKey;type:varchar(13)" json:"code"`
	Name       string  `gorm:"type:varchar(100);not null" json:"name"`
	Level      int     `gorm:"not null" json:"level"`
	ParentCode *string `gorm:"type:varchar(13);index" json:"parent_code"`
}

func (Region) TableName() string { return "regions" }
