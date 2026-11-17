package main

import (
	"os"
	"strings"
	"testing"
)

func TestNormalizeNumber(t *testing.T) {
	cases := map[string]string{
		"06 12 34 56 78":                     "0612345678",
		"06.12.34.56.78":                     "0612345678",
		"06-12-34-56-78":                     "0612345678",
		"6 12 34 56 78":                      "0612345678",
		"612345678":                          "0612345678",
		"+33.6.12.34.56.78":                  "0612345678",
		"+33 6 12 34 56 78":                  "0612345678",
		"+33612345678":                       "0612345678",
		"+33 (0)6 12 34 56 78":               "0612345678",
		"+33 06 12 34 56 78":                 "0612345678",
		"00336 12 34 56 78":                  "0612345678",
		"0033.6.12.34.56.78":                 "0612345678",
		"0033 06 12 34 56 78":                "0612345678",
		"0033612345678":                      "0612345678",
		"33612345678":                        "0612345678",
		"33 6 12 34 56 78":                   "0612345678",
		"33.6.12.34.56.78":                   "0612345678",
		"330612345678":                       "0612345678",
		"1010":                               "1010",
		"118218":                             "118218",
		"+33700500000000":                    "0700500000000",
		"+33 700500000000":                   "0700500000000",
		"33700500000000":                     "0700500000000",
		"0033700500000000":                   "0700500000000",
		"+337005000000000":                   "07005000000000",
		"337005000000000":                    "07005000000000",
		"00337005000000000":                  "07005000000000",
		"0700500000000":                      "0700500000000",
		"06\u00a012\u00a034\u00a056\u00a078": "0612345678",
	}
	for raw, expected := range cases {
		got, err := normalizeNumber(raw)
		if err != nil {
			t.Errorf("normalizeNumber(%q) error: %v", raw, err)
			continue
		}
		if got != expected {
			t.Errorf("normalizeNumber(%q) = %q, want %q", raw, got, expected)
		}
	}
}

func TestNormalizeNumberInvalid(t *testing.T) {
	for _, raw := range []string{"", "abc", "+590 590 12 34 56", "00590 590 12 34 56", "06 12 34 56 7X"} {
		if _, err := normalizeNumber(raw); err == nil {
			t.Errorf("normalizeNumber(%q) should have failed", raw)
		}
	}
}

func TestNumberType(t *testing.T) {
	cases := map[string]string{
		"0612345678":    "Mobile",
		"0140000000":    "Geographic landline",
		"0800123456":    "Special / value-added service",
		"0900000000":    "Non-geographic",
		"1010":          "Short number",
		"0700500000000": "Extended-length mobile",
	}
	for number, expected := range cases {
		if got := numberType(number); got != expected {
			t.Errorf("numberType(%q) = %q, want %q", number, got, expected)
		}
	}
}

func TestSplitList(t *testing.T) {
	got := splitList("0612345678, 0140000000;1010")
	expected := []string{"0612345678", "0140000000", "1010"}
	if strings.Join(got, "|") != strings.Join(expected, "|") {
		t.Errorf("splitList = %v, want %v", got, expected)
	}
}

func TestReadNumbers(t *testing.T) {
	got, err := readNumbers(strings.NewReader("# commentaire\n0612345678\n0140000000, 1010 # fin\n"))
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"0612345678", "0140000000", "1010"}
	if strings.Join(got, "|") != strings.Join(expected, "|") {
		t.Errorf("readNumbers = %v, want %v", got, expected)
	}
}

func TestDatabase(t *testing.T) {
	if _, err := os.Stat("data/MAJNUM.csv"); err != nil {
		t.Skip("data missing")
	}
	database, err := loadDatabase("data")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		number     string
		code       string
		brand      string
		identity   string
		wantFrozen bool
		wantAlloc  bool
	}{
		{"0612345678", "SFR0", "SFR", "Société française du radiotéléphone", false, true},
		{"0140000000", "FRTE", "Orange", "Orange", false, true},
		{"1010", "FRTE", "Orange", "Orange", false, true},
		{"118218", "LNUM", "118 218 Le numéro", "118 218 Le numéro", false, true},
		{"0700500000000", "DAUF", "Dauphin telecom", "Dauphin telecom", false, true},
		{"0700000000", "", "", "", false, false},
		{"0375523123", "", "", "", true, false},
	}
	for _, test := range tests {
		lookup := database.Lookup(test.number)
		if (lookup.Allocation != nil) != test.wantAlloc {
			t.Errorf("%s: allocation = %v, want %v", test.number, lookup.Allocation != nil, test.wantAlloc)
			continue
		}
		if (lookup.Frozen != nil) != test.wantFrozen {
			t.Errorf("%s: frozen = %v, want %v", test.number, lookup.Frozen != nil, test.wantFrozen)
		}
		if lookup.Allocation == nil {
			continue
		}
		if lookup.Allocation.Code != test.code {
			t.Errorf("%s: code = %q, want %q", test.number, lookup.Allocation.Code, test.code)
		}
		if lookup.Allocation.Brand() != test.brand {
			t.Errorf("%s: brand = %q, want %q", test.number, lookup.Allocation.Brand(), test.brand)
		}
		if lookup.Allocation.Identity != test.identity {
			t.Errorf("%s: identity = %q, want %q", test.number, lookup.Allocation.Identity, test.identity)
		}
	}

	sfr := database.Lookup("0612345678")
	if sfr.Allocation.Territory != "Métropole" {
		t.Errorf("territory = %q", sfr.Allocation.Territory)
	}
	if sfr.Allocation.Date != "08/12/2011" {
		t.Errorf("date = %q", sfr.Allocation.Date)
	}
	if database.AllocCount < 20000 {
		t.Errorf("AllocCount = %d", database.AllocCount)
	}
	if database.OperatorCount < 1000 {
		t.Errorf("OperatorCount = %d", database.OperatorCount)
	}
}

func TestRenderOneline(t *testing.T) {
	database, err := loadDatabase("data")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderOneline(database.Lookup("0612345678"), false); got != "0612345678  SFR (SFR0)" {
		t.Errorf("renderOneline = %q", got)
	}
	expectedFull := "0612345678 | SFR (SFR0) - Société française du radiotéléphone | Mobile | Métropole | 06 12 00 00 00 - 06 12 99 99 99 | 08/12/2011"
	if got := renderOneline(database.Lookup("0612345678"), true); got != expectedFull {
		t.Errorf("renderOneline full = %q\nwant %q", got, expectedFull)
	}
	if got := renderOneline(database.Lookup("0700000000"), false); got != "0700000000  not allocated" {
		t.Errorf("renderOneline = %q", got)
	}
	if got := renderOneline(database.Lookup("0700000000"), true); got != "0700000000 | not allocated | Mobile | - | - | -" {
		t.Errorf("renderOneline full = %q", got)
	}
	if got := renderOneline(database.Lookup("0375523123"), false); !strings.Contains(got, "frozen: Numéros polyvalents") {
		t.Errorf("renderOneline = %q", got)
	}
	short := "1010 | Orange (FRTE) | Short number | National | 1010 | 01/01/2017"
	if got := renderOneline(database.Lookup("1010"), true); got != short {
		t.Errorf("renderOneline full = %q\nwant %q", got, short)
	}
}

func TestRenderTable(t *testing.T) {
	database, err := loadDatabase("data")
	if err != nil {
		t.Fatal(err)
	}
	output := renderTable(database.Lookup("0612345678"), database)
	for _, expected := range []string{
		"ARCEP result",
		"06 12 34 56 78",
		"Assigned operator",
		"SFR",
		"Société française du radiotéléphone",
		"06 12 00 00 00 - 06 12 99 99 99",
		"Allocated",
		"portability",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("table does not contain %q:\n%s", expected, output)
		}
	}
}

func TestFrozenPrefixLengthFiltering(t *testing.T) {
	database, err := loadDatabase("data")
	if err != nil {
		t.Fatal(err)
	}
	// 0700000000 is a 10-digit mobile number, not an extended-length number:
	// the frozen 07000000 prefix (extended length) must not apply.
	if lookup := database.Lookup("0700000000"); lookup.Frozen != nil {
		t.Errorf("0700000000 should not be frozen: %v", lookup.Frozen)
	}
	if lookup := database.Lookup("0700500000000"); lookup.Allocation == nil {
		t.Error("0700500000000 should be allocated")
	}
}
