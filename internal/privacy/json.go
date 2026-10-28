package privacy

import (
	"bytes"
	"encoding/json"
)

// orderFields: kunci JSON berisi data pasien di objek apa pun (order, rute
// kurir, transaksi, bukti terima, ...), di kedalaman mana pun.
var orderFields = map[string]func(string) string{
	"customer_name":     Name,
	"patient_name":      Name,
	"receiver_name":     Name,
	"customer_phone":    Phone,
	"medical_record_no": MedicalRecordNo,
	"dropoff_address":   Address,
}

// customerFields: kunci umum milik pelanggan tersimpan. Hanya disamarkan pada
// objek yang memang pelanggan (punya "order_count"), supaya nama layanan, user,
// atau RS tidak ikut tersamar.
var customerFields = map[string]func(string) string{
	"name":    Name,
	"phone":   Phone,
	"address": Address,
}

// MaskJSON menyamarkan data pasien di sebuah body JSON. Angka dibiarkan apa
// adanya (json.Number), jadi hanya teks pasien yang berubah.
func MaskJSON(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	walk(v)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func walk(v any) {
	switch t := v.(type) {
	case map[string]any:
		_, isCustomer := t["order_count"]
		for k, val := range t {
			if s, ok := val.(string); ok {
				if mask, ok := orderFields[k]; ok {
					t[k] = mask(s)
				} else if mask, ok := customerFields[k]; ok && isCustomer {
					t[k] = mask(s)
				}
				continue
			}
			walk(val)
		}
	case []any:
		for _, item := range t {
			walk(item)
		}
	}
}
