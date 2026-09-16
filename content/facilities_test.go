package content_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
)

// A property's venues - its bars, restaurants and meeting rooms - used to reach
// a caller as a row of zeros. Amadeus publishes them as named entries with their
// own prose and photographs, and the mapper read only a summary counter that the
// payload does not carry, so a hotel with a bar reported having none.

// The captured property publishes two restaurants and NO quantity, which is the
// shape that produced the zero.
func TestRestaurantsAreCountedFromTheVenuesThemselves(t *testing.T) {
	hotel := fetch(t)
	if hotel.Facilities == nil || hotel.Facilities.Restaurants == nil {
		t.Fatal("the captured property publishes restaurants, and none were mapped")
	}
	r := hotel.Facilities.Restaurants
	if len(r.Venues) != 2 {
		t.Fatalf("mapped %d venues, want 2", len(r.Venues))
	}
	if r.Count != 2 {
		t.Errorf("Count = %d, want 2 - Amadeus states the venues and no quantity", r.Count)
	}
}

// The names and categories are the whole point: without them there is nothing to
// put on a page.
func TestRestaurantsKeepTheirNamesAndCategories(t *testing.T) {
	venues := fetch(t).Facilities.Restaurants.Venues
	names := map[string]content.Restaurant{}
	for _, v := range venues {
		names[v.Name] = v
	}
	for _, want := range []string{"test restaurant", "Test bar"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("venue %q was dropped; got %v", want, names)
		}
	}
	if got := names["test restaurant"].Category; got != "BAR_OR_LOUNGE" {
		t.Errorf("category = %q, want BAR_OR_LOUNGE", got)
	}
	if got := names["Test bar"].MaxSeating; got != 20 {
		t.Errorf("MaxSeating = %d, want 20", got)
	}
}

// The meals and cuisines a venue serves are per-venue, not a property-wide blur.
func TestARestaurantKeepsItsMealsAndCuisines(t *testing.T) {
	venues := fetch(t).Facilities.Restaurants.Venues
	var dining content.Restaurant
	for _, v := range venues {
		if v.Name == "test restaurant" {
			dining = v
		}
	}
	if !dining.ServesBreakfast || !dining.ServesBrunch || !dining.ServesLunch || !dining.ServesDinner {
		t.Errorf("meals = %+v, want all four", dining)
	}
	if len(dining.Cuisines) != 1 || dining.Cuisines[0] != "FRENCH" {
		t.Errorf("Cuisines = %v, want [FRENCH]", dining.Cuisines)
	}
}

// Amadeus files the prose inside the venue's MEDIA array, not in its description
// field. Reading the field alone returned nothing for a venue that plainly had
// prose - the same trap the property description fell into.
func TestVenueProseIsRecoveredFromTheMediaArray(t *testing.T) {
	venues := fetch(t).Facilities.Restaurants.Venues
	for _, v := range venues {
		if v.Description == "" {
			t.Errorf("%q came back with no description, though its media carries one", v.Name)
		}
	}
	if got := fetch(t).Facilities.Restaurants.Description; got == "" {
		t.Error("the dining summary has no description either")
	}
}

// The meeting-room half of the same bug, in the shape the live sandbox returns
// for RTPARDAM: one named room, no quantity, and the prose inside the media.
const meetingRoomPayload = `{"data":{"basic":{"hotelId":"RTPARDAM","name":"Mercure"},
"facilities":{"meetingRoomInfo":{"meetingRooms":[
 {"name":"SORBONNE","meetingRoomType":"BOARDROOM","roomDimensions":{"area":30,"areaUnit":"SQUARE_METER"},
  "occupancyPerLayouts":[{"layout":"U_SHAPE","maxOccupancy":6}],
  "media":[{"description":{"lang":"en","text":"323 sq. ft. meeting room."}}]}]}}}}`

func meetingRoomHotel(t *testing.T) *content.Hotel {
	t.Helper()
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK, meetingRoomPayload)
	hotel, err := content.NewService(server.Client()).Get(context.Background(), content.Query{HotelID: "RTPARDAM"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return hotel
}

func TestMeetingRoomsAreCountedFromTheRoomsThemselves(t *testing.T) {
	rooms := meetingRoomHotel(t).Facilities.MeetingRooms
	if rooms == nil {
		t.Fatal("the property publishes a meeting room, and none was mapped")
	}
	if rooms.Count != 1 {
		t.Errorf("Count = %d, want 1 - Amadeus states the room and no quantity", rooms.Count)
	}
	if len(rooms.Rooms) != 1 {
		t.Fatalf("mapped %d rooms, want 1", len(rooms.Rooms))
	}
}

func TestAMeetingRoomKeepsItsNameLayoutAndProse(t *testing.T) {
	room := meetingRoomHotel(t).Facilities.MeetingRooms.Rooms[0]
	if room.Name != "SORBONNE" {
		t.Errorf("Name = %q, want SORBONNE", room.Name)
	}
	if room.Type != "BOARDROOM" {
		t.Errorf("Type = %q, want BOARDROOM", room.Type)
	}
	if !strings.Contains(room.Description, "323 sq. ft.") {
		t.Errorf("Description = %q, want the prose from the media array", room.Description)
	}
	if room.Dimensions == nil || room.Dimensions.Area != 30 {
		t.Errorf("Dimensions = %+v, want an area of 30", room.Dimensions)
	}
	if len(room.Capacities) != 1 || room.Capacities[0].Layout != "U_SHAPE" || room.Capacities[0].MaxOccupancy != 6 {
		t.Errorf("Capacities = %+v, want one U_SHAPE seating 6", room.Capacities)
	}
}

// When Amadeus DOES state a quantity it is the authority: it knows about rooms
// it did not trouble to describe, so the count must not shrink to the list.
func TestAStatedQuantityWinsOverTheListLength(t *testing.T) {
	const payload = `{"data":{"basic":{"hotelId":"RTPARSOR"},"facilities":{"meetingRoomInfo":{"quantity":9,
	"meetingRooms":[{"name":"SORBONNE"}]}}}}`
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK, payload)
	hotel, err := content.NewService(server.Client()).Get(context.Background(), content.Query{HotelID: "RTPARSOR"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := hotel.Facilities.MeetingRooms.Count; got != 9 {
		t.Errorf("Count = %d, want the stated 9", got)
	}
}
