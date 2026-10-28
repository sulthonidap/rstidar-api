// Package privacy menyamarkan data pasien (masking) sebelum dikirim ke panel.
// Data lengkap hanya keluar lewat aplikasi kurir (untuk order miliknya) dan
// endpoint label/resi.
package privacy

import (
	"strings"
	"unicode"
)

// maskRunes mengganti huruf & angka dengan '*', kecuali keepStart huruf/angka
// pertama dan keepEnd terakhir. Spasi dan tanda baca dibiarkan supaya bentuk
// teksnya masih terbaca ("Jl. P***** **. **").
func maskRunes(s string, keepStart, keepEnd int) string {
	r := []rune(s)
	total := 0
	for _, ch := range r {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			total++
		}
	}
	seen := 0
	for i, ch := range r {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) {
			continue
		}
		if seen >= keepStart && seen < total-keepEnd {
			r[i] = '*'
		}
		seen++
	}
	return string(r)
}

// Name: huruf pertama tiap kata — "Budi Santoso" → "B*** S******".
func Name(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = maskRunes(w, 1, 0)
	}
	return strings.Join(words, " ")
}

// Phone: 4 digit depan + 3 belakang — "081234567890" → "0812*****890".
func Phone(s string) string {
	if len([]rune(s)) <= 7 {
		return maskRunes(s, 2, 0)
	}
	return maskRunes(s, 4, 3)
}

// MedicalRecordNo: 2 karakter terakhir — "00123456" → "******56".
func MedicalRecordNo(s string) string { return maskRunes(s, 0, 2) }

// Address: 4 huruf pertama, sisanya disamarkan kecuali bagian setelah koma
// terakhir (biasanya kota) — "Jl. Pemuda 12, Magelang" → "Jl. P***** **, Magelang".
func Address(s string) string {
	head, tail := s, ""
	if i := strings.LastIndex(s, ","); i > 0 {
		head, tail = s[:i], s[i:]
	}
	return maskRunes(head, 4, 0) + tail
}

// IsMasked: teks ini hasil masking (berisi '*'). Dipakai supaya nilai tersamar
// yang dikirim balik oleh form edit tidak menimpa data asli.
func IsMasked(s string) bool { return strings.ContainsRune(s, '*') }

// Keep mengembalikan nilai yang harus disimpan saat form mengirim balik field
// yang tadinya tersamar: kalau incoming sama dengan versi samaran dari nilai
// asli, nilai asli dipertahankan. ok=false kalau incoming masih berisi '*'
// tapi bukan samaran yang utuh (field diedit setengah) — harus diisi ulang.
func Keep(incoming, original string, mask func(string) string) (string, bool) {
	if !IsMasked(incoming) {
		return incoming, true
	}
	if original != "" && incoming == mask(original) {
		return original, true
	}
	return incoming, false
}
