package privacy

import (
	"strings"
	"testing"
)

func TestMaskers(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"name", Name("Budi Santoso"), "B*** S******"},
		{"name single letter", Name("A"), "A"},
		{"phone", Phone("081234567890"), "0812*****890"},
		{"phone short", Phone("12345"), "12***"},
		{"mrn", MedicalRecordNo("00123456"), "******56"},
		{"address keeps city", Address("Jl. Pemuda 12, Magelang"), "Jl. Pe**** **, Magelang"},
		{"address no comma", Address("Jl. Pemuda 12"), "Jl. Pe**** **"},
		{"empty", Name(""), ""},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestKeep(t *testing.T) {
	orig := "Budi Santoso"
	if v, ok := Keep(Name(orig), orig, Name); !ok || v != orig {
		t.Errorf("unchanged masked value should restore original, got %q %v", v, ok)
	}
	if v, ok := Keep("Budi S.", orig, Name); !ok || v != "Budi S." {
		t.Errorf("typed value should win, got %q %v", v, ok)
	}
	if _, ok := Keep("B*** Santoso", orig, Name); ok {
		t.Error("half-edited masked value must be rejected")
	}
}

func TestMaskJSON(t *testing.T) {
	in := `{"data":[{"id":7,"price":15000.5,"customer_name":"Budi Santoso","customer_phone":"081234567890",
		"service":{"name":"Reguler"},"delivery":{"receiver_name":"Siti"},
		"organization":{"name":"RSUD Tidar","phone":"0293123"}}],
		"customers":[{"name":"Ani","phone":"081299998888","address":"Jl. Ahmad Yani, Magelang","order_count":2}]}`
	out, err := MaskJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"customer_name":"B*** S******"`, `"customer_phone":"0812*****890"`,
		`"receiver_name":"S***"`, `"name":"Reguler"`, `"name":"RSUD Tidar"`, `"phone":"0293123"`,
		`"name":"A**"`, `"address":"Jl. Ah*** ****, Magelang"`, `"price":15000.5`, `"id":7`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}
