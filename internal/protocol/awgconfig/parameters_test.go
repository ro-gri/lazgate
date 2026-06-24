package awgconfig

import "testing"

func TestParameters(t *testing.T) {
	headers := [4]string{"1-100", "101", "102", "4294967295"}
	if err := Validate(headers, [5]string{"<r 2><b 0x1234><rd 5><rc 8><t>"}, "32-128", map[string]string{"RekeyTimeout": "2-5"}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"<r 0>", "<r 65508>", "<b 0x123>", "<b 0xgg>", "<t>garbage"} {
		if ValidateCPS(value) == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	for _, value := range []string{"4294967296", "5-4", "4294967296-4294967296"} {
		if _, _, err := Range(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if err := Validate(headers, [5]string{}, "65536", nil); err == nil {
		t.Fatal("unsupported tool padding accepted")
	}
	if err := Validate(headers, [5]string{}, "", map[string]string{"Unknown": "2"}); err == nil {
		t.Fatal("unknown timing accepted")
	}
}
