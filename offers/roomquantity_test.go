package offers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// TestRoomQuantityReflectsWhatWasRequested pins the rule that decides what
// Offer.RoomQuantity holds, because it is the SDK's most surprising number.
//
// Amadeus does not volunteer roomQuantity. It echoes the value it was sent, and
// omits the field entirely when it was sent none - verified against the live
// sandbox on the same property and dates, where adults=4&roomQuantity=4 put
// "roomQuantity":4 on every offer and adults=4 alone put the field on none of
// them. Both captured fixtures agree: not one of the 61 offers in search.json
// carries it, and neither does offer-by-id.json, since both were captured
// without asking.
//
// The consequence catches callers out. Search with Guests{Adults: 4} and no
// Rooms, and every offer comes back reporting RoomQuantity 0 rather than the
// four rooms that were wanted - not because the SDK dropped it, but because
// nothing ever asked for four rooms. Reading the field as "how many rooms this
// offer covers" is only safe when SearchQuery.Rooms was set.
func TestRoomQuantityReflectsWhatWasRequested(t *testing.T) {
	// respondWith is the response shape Amadeus sends in each case: the offer
	// carries roomQuantity when it was requested, and lacks the field when it
	// was not.
	for _, tc := range []struct {
		name        string
		query       offers.SearchQuery
		respondWith string
		wantSent    string
		wantMapped  int
	}{
		{
			name: "requested, so echoed",
			query: offers.SearchQuery{
				HotelIDs: []string{"RTPARSUR"},
				Guests:   offers.Guests{Adults: 4},
				Rooms:    4,
			},
			respondWith: `"roomQuantity":4,`,
			wantSent:    "4",
			wantMapped:  4,
		},
		{
			name: "not requested, so absent",
			query: offers.SearchQuery{
				HotelIDs: []string{"RTPARSUR"},
				Guests:   offers.Guests{Adults: 4},
			},
			respondWith: "",
			wantSent:    "",
			wantMapped:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := amadeustest.New(t)
			server.JSON(http.MethodGet, searchPath, http.StatusOK, `{"data":[{
			  "type":"hotel-offers",
			  "hotel":{"hotelId":"RTPARSUR","name":"Novotel Paris Suresnes Longchamp"},
			  "available":true,
			  "offers":[{"id":"NF92EK5ZIK",`+tc.respondWith+`
			             "guests":{"adults":4},
			             "price":{"currency":"EUR","total":"415.80"}}]}]}`)

			results, err := offers.NewService(server.Client()).Search(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(results) != 1 || len(results[0].Offers) != 1 {
				t.Fatalf("the response was lost: %+v", results)
			}

			if got := server.LastRequest(t).Query.Get("roomQuantity"); got != tc.wantSent {
				t.Errorf("sent roomQuantity=%q, want %q", got, tc.wantSent)
			}
			if got := results[0].Offers[0].RoomQuantity; got != tc.wantMapped {
				t.Errorf("Offer.RoomQuantity = %d, want %d", got, tc.wantMapped)
			}
		})
	}
}

// TestCapturedFixturesStateNoRoomQuantity guards the premise the test above
// rests on: the fixtures were captured without requesting rooms, so every offer
// in them must report zero.
//
// It is worth asserting rather than assuming. If a future capture asks for
// rooms, these fixtures start carrying the field, and a reader comparing them
// against the rule above would conclude the rule had changed when only the
// capture did.
func TestCapturedFixturesStateNoRoomQuantity(t *testing.T) {
	service, _ := newService(t)

	results, err := service.Search(context.Background(), offers.SearchQuery{
		HotelIDs: []string{"RTPAREIF"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	offerCount := 0
	for _, result := range results {
		for _, offer := range result.Offers {
			offerCount++
			if offer.RoomQuantity != 0 {
				t.Errorf("offer %s reports RoomQuantity %d, but the fixture was captured "+
					"without a roomQuantity parameter; re-check the rule in "+
					"TestRoomQuantityReflectsWhatWasRequested", offer.ID, offer.RoomQuantity)
			}
		}
	}
	if offerCount == 0 {
		t.Fatal("the fixture yielded no offers to check")
	}
	t.Logf("%d fixture offers, every one with roomQuantity unstated", offerCount)
}
