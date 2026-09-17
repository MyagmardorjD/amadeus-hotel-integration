package content_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
)

// Three blocks the DTO decoded and the mapper then threw away: a venue's opening
// hours, how to reach a point of interest, and most of what Amadeus counts about
// the building.

const richPayload = `{"data":{
"basic":{"hotelId":"RTPARMON","name":"Mercure"},
"hotel":{"building":{"architectureCode":"MODERN","numberOfFloors":7,"numberOfRooms":67,
  "numberOfExecutiveFloors":2,"numberOfBuildings":1,"numberOfElevators":3,"renovationDate":"2014"}},
"facilities":{"restaurantInfo":{"restaurants":[
  {"name":"JOSEPHINE","category":"BAR_OR_LOUNGE","isReservationRequired":true,
   "operatingHours":{"startTime":"06:30:00","endTime":"01:00:00",
     "byDays":[{"day":"MON"},{"day":"SAT"},{"day":""}]}}]}},
"pointOfInterest":[
  {"basic":{"name":"GARE DU NORD"},"categoryCode":"RAIL_STATION",
   "transportations":[
     {"transportMode":"SUBWAY","description":"Line 4 from Cadet","isReservationRequired":false,
      "operatingHours":{"startTime":"05:30:00","endTime":"00:45:00","byDays":[{"day":"MON"}]}},
     {"transportMode":"TAXI","description":"About ten minutes"}]}]}}`

func richHotel(t *testing.T) *content.Hotel {
	t.Helper()
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK, richPayload)
	hotel, err := content.NewService(server.Client()).Get(context.Background(), content.Query{HotelID: "RTPARMON"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return hotel
}

// Floors and rooms were kept; the executive floors, the buildings and the lifts
// were not, though Amadeus counts all five in the same block.
func TestBuildingKeepsEveryCountAmadeusStates(t *testing.T) {
	b := richHotel(t).Building
	if b == nil {
		t.Fatal("the property states a building and none was mapped")
	}
	if b.Floors != 7 || b.TotalRooms != 67 {
		t.Errorf("Floors/TotalRooms = %d/%d, want 7/67", b.Floors, b.TotalRooms)
	}
	if b.ExecutiveFloors != 2 || b.Buildings != 1 || b.Elevators != 3 {
		t.Errorf("executive/buildings/elevators = %d/%d/%d, want 2/1/3",
			b.ExecutiveFloors, b.Buildings, b.Elevators)
	}
}

// The architecture code is a code word. Returning it as Description handed a
// client rendering a paragraph the single word "MODERN".
func TestArchitectureIsNotPresentedAsProse(t *testing.T) {
	b := richHotel(t).Building
	if b.Architecture != "MODERN" {
		t.Errorf("Architecture = %q, want MODERN", b.Architecture)
	}
	if b.Description != "" {
		t.Errorf("Description = %q, want empty - Amadeus publishes no prose here", b.Description)
	}
}

// A restaurant nobody can tell the opening times of is half a listing.
func TestAVenueKeepsItsOpeningHours(t *testing.T) {
	venues := richHotel(t).Facilities.Restaurants.Venues
	if len(venues) != 1 {
		t.Fatalf("mapped %d venues, want 1", len(venues))
	}
	venue := venues[0]
	if !venue.ReservationRequired {
		t.Error("ReservationRequired = false, want true")
	}
	if venue.Hours == nil {
		t.Fatal("the venue states opening hours and none were mapped")
	}
	if venue.Hours.StartTime != "06:30:00" || venue.Hours.EndTime != "01:00:00" {
		t.Errorf("hours = %s-%s, want 06:30:00-01:00:00", venue.Hours.StartTime, venue.Hours.EndTime)
	}
	// The wire wraps each weekday in an object of its own, and an empty one is
	// noise rather than a day.
	if len(venue.Hours.Days) != 2 || venue.Hours.Days[0] != "MON" || venue.Hours.Days[1] != "SAT" {
		t.Errorf("Days = %v, want [MON SAT]", venue.Hours.Days)
	}
}

// A point of interest with a distance and no way of getting there is half an
// answer; Amadeus lists the modes and we dropped them.
func TestAPointOfInterestKeepsHowToReachIt(t *testing.T) {
	pois := richHotel(t).PointsOfInterest
	if len(pois) != 1 {
		t.Fatalf("mapped %d points of interest, want 1", len(pois))
	}
	transport := pois[0].Transportations
	if len(transport) != 2 {
		t.Fatalf("mapped %d transportations, want 2", len(transport))
	}
	if transport[0].Mode != "SUBWAY" || transport[0].Description != "Line 4 from Cadet" {
		t.Errorf("first = %+v, want the subway", transport[0])
	}
	if transport[0].Hours == nil || transport[0].Hours.StartTime != "05:30:00" {
		t.Errorf("subway hours = %+v, want them mapped", transport[0].Hours)
	}
	// The taxi states no hours, and an empty schedule is nil rather than a
	// struct of blanks a client would render as "open 00:00-00:00".
	if transport[1].Mode != "TAXI" {
		t.Errorf("second = %+v, want the taxi", transport[1])
	}
	if transport[1].Hours != nil {
		t.Errorf("taxi hours = %+v, want nil", transport[1].Hours)
	}
}
