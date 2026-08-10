package apierr_test

import (
	"encoding/json"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
)

func TestDetailAcceptsQuotedAndBareNumbers(t *testing.T) {
	// Amadeus is not consistent about this. The shopping endpoints send
	//   {"code": 477, "status": 400}
	// while the authentication and header errors documented in the Enterprise
	// guide send
	//   {"code": "38191", "status": "401"}
	// Rejecting either form loses the whole errors array, and with it the only
	// explanation of what went wrong.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"bare numbers", `{"code":477,"status":400,"title":"INVALID FORMAT"}`},
		{"quoted numbers", `{"code":"477","status":"400","title":"INVALID FORMAT"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d apierr.Detail
			if err := json.Unmarshal([]byte(tc.body), &d); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if d.Code != 477 {
				t.Errorf("Code = %d, want 477", d.Code)
			}
			if d.Status != 400 {
				t.Errorf("Status = %d, want 400", d.Status)
			}
			if d.Title != "INVALID FORMAT" {
				t.Errorf("Title = %q", d.Title)
			}
		})
	}
}

func TestDetailKeepsTheRestOfTheErrorWhenACodeIsUnparseable(t *testing.T) {
	// A code that is neither a number nor a numeric string must not cost us the
	// title and detail, which are the human-readable part.
	var d apierr.Detail
	if err := json.Unmarshal([]byte(`{"code":"N/A","title":"ODD","detail":"something"}`), &d); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if d.Title != "ODD" || d.Detail != "something" {
		t.Errorf("lost the readable fields: %+v", d)
	}
	if d.Code != 0 {
		t.Errorf("Code = %d, want 0 for an unparseable code", d.Code)
	}
}

func TestDetailDecodesSourceAndDocumentation(t *testing.T) {
	// The custom unmarshaller must not drop the fields it does not special-case.
	var d apierr.Detail
	body := `{"code":572,"title":"T","source":{"parameter":"cityCode","example":"PAR"},
	          "documentation":"https://example.invalid"}`
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if d.Source.Parameter != "cityCode" || d.Source.Example != "PAR" {
		t.Errorf("Source = %+v", d.Source)
	}
	if d.Documentation != "https://example.invalid" {
		t.Errorf("Documentation = %q", d.Documentation)
	}
}
