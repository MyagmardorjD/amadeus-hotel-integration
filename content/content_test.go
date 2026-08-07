package content_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
)

const contentPath = "/v3/reference-data/locations/by-hotel"

// These run against hotel.json, captured from the live Amadeus sandbox by
// internal/capture. Two of them exist because the real payload disproved
// assumptions the hand-written fixture had baked in - see the comments on
// TestDescriptionIsRecoveredFromTheMediaArray and TestPrimaryPhotoIsAnActualPhoto.

func newService(t *testing.T) (content.Service, *amadeustest.Server) {
	t.Helper()
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, contentPath, "hotel")
	return content.NewService(server.Client()), server
}

func fetch(t *testing.T) *content.Hotel {
	t.Helper()
	service, _ := newService(t)

	hotel, err := service.Get(context.Background(), content.Query{HotelID: "HNPARKGU"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return hotel
}

func TestHotelIdentityIsMapped(t *testing.T) {
	hotel := fetch(t)

	if hotel.ID == "" || hotel.Name == "" {
		t.Errorf("hotel = %q / %q", hotel.ID, hotel.Name)
	}
	if hotel.ChainCode == "" {
		t.Errorf("%s has no chain code", hotel.ID)
	}
	if hotel.Rating != "" && !hotel.Rating.IsValid() {
		t.Errorf("Rating %q is not 1-5", hotel.Rating)
	}
	if len(hotel.Categories) == 0 && len(hotel.Segments) == 0 {
		t.Error("neither categories nor segments were mapped")
	}
}

// The property description does not arrive in basic.description - Amadeus
// leaves that null. It arrives as entries in the media array that carry text
// and a tag but no image. The first implementation read the empty field and
// dumped all the prose into Media, losing the description entirely. Only real
// captured data revealed this.
func TestDescriptionIsRecoveredFromTheMediaArray(t *testing.T) {
	hotel := fetch(t)

	if hotel.Description == nil || hotel.Description.IsEmpty() {
		t.Fatal("the property description was dropped; it lives in the media array, not basic.description")
	}
	if len(hotel.Descriptions) == 0 {
		t.Fatal("no prose blocks were captured")
	}

	// Each prose block must carry what it describes, taken from its tag.
	for _, text := range hotel.Descriptions {
		if text.Value == "" {
			t.Error("a prose block has no text")
		}
		if text.Type == "" {
			t.Errorf("prose block %.30q has no type; the media tag should supply it", text.Value)
		}
	}

	// The long description is what a listing page wants, so it should win.
	if _, ok := hotel.DescriptionOf("HOTEL_LONG_DESCRIPTION"); ok {
		if !strings.Contains(hotel.Description.Type, "LONG") {
			t.Errorf("Description came from %q while a long description exists", hotel.Description.Type)
		}
	}
}

func TestDescriptionOfLooksUpByTag(t *testing.T) {
	hotel := fetch(t)

	short, ok := hotel.DescriptionOf("HOTEL_SHORT_DESCRIPTION")
	if !ok {
		t.Skip("the captured property published no short description")
	}
	if short.Value == "" {
		t.Error("the short description is empty")
	}
	if _, ok := hotel.DescriptionOf("NOT_A_REAL_TAG"); ok {
		t.Error("an unknown tag should not match")
	}
}

// Amadeus never sets "type" on a media entry, so the SDK infers the kind from
// whether the entry actually carries an image. Without that, PrimaryPhoto
// returned a text block whose URL was empty - an <img> with no src.
func TestPrimaryPhotoIsAnActualPhoto(t *testing.T) {
	hotel := fetch(t)

	photo, ok := hotel.PrimaryPhoto()
	if !ok {
		t.Skip("the captured property published no photographs")
	}

	if !photo.IsVisual() {
		t.Fatal("PrimaryPhoto returned an entry carrying no image")
	}
	if photo.Best(400) == "" {
		t.Error("the primary photo resolves to an empty URL")
	}

	// Every entry in Media must be a real image; prose belongs in Descriptions.
	for _, asset := range hotel.Media {
		if !asset.IsVisual() {
			t.Errorf("media entry %q carries no image and should have been split out", asset.ID)
		}
	}
}

func TestBestPicksARenditionRatherThanTheOriginal(t *testing.T) {
	hotel := fetch(t)

	photo, ok := hotel.PrimaryPhoto()
	if !ok || len(photo.Scales) < 2 {
		t.Skip("the captured property has no multi-scale photograph")
	}

	// Widest rendition that still fits the target.
	small := photo.Best(200)
	large := photo.Best(4000)
	if small == "" || large == "" {
		t.Fatalf("Best returned empty: small=%q large=%q", small, large)
	}

	widthOf := func(url string) int {
		for _, scale := range photo.Scales {
			if scale.URL == url && scale.Dimensions != nil {
				return scale.Dimensions.Width
			}
		}
		return 0
	}
	if w := widthOf(small); w > 200 && w != 0 {
		t.Errorf("Best(200) returned a %dpx rendition", w)
	}
	if widthOf(large) < widthOf(small) {
		t.Error("Best(4000) returned something smaller than Best(200)")
	}
}

func TestContactsAreMapped(t *testing.T) {
	hotel := fetch(t)
	if len(hotel.Contacts) == 0 {
		t.Skip("the captured property published no contacts")
	}

	usable := 0
	for _, contact := range hotel.Contacts {
		if contact.Address != nil || len(contact.Phones) > 0 ||
			len(contact.Emails) > 0 || contact.Website != "" {
			usable++
		}
	}
	if usable == 0 {
		t.Error("no contact carries anything usable; empty ones should have been dropped")
	}
}

func TestPhoneNumberIsAssembledFromItsParts(t *testing.T) {
	// Amadeus splits a number across countryCallingCode, areaCode and number,
	// and the bare number is frequently unusable on its own.
	hotel := fetch(t)

	for _, contact := range hotel.Contacts {
		for _, phone := range contact.Phones {
			if phone.Number == "" {
				t.Error("a phone entry mapped to an empty number")
			}
			if phone.Category == "" {
				t.Errorf("phone %q has no category", phone.Number)
			}
		}
	}
}

func TestPositionIsExposedThroughLocation(t *testing.T) {
	hotel := fetch(t)

	position, ok := hotel.Position()
	if !ok {
		t.Skip("the captured property has no coordinates")
	}
	if err := position.Validate(); err != nil {
		t.Errorf("invalid coordinates: %v", err)
	}
}

func TestAmenitiesCarryTheirCharges(t *testing.T) {
	hotel := fetch(t)

	all := append(append([]content.Amenity{}, hotel.Amenities...), facilityAmenities(hotel)...)
	if len(all) == 0 {
		t.Skip("the captured property published no amenities")
	}

	for _, amenity := range all {
		if amenity.Code == "" {
			t.Errorf("amenity with no code: %+v", amenity)
		}
	}

	if hotel.HasAmenity("DEFINITELY_NOT_A_REAL_AMENITY") {
		t.Error("HasAmenity matched a code the property does not have")
	}
}

func facilityAmenities(h *content.Hotel) []content.Amenity {
	if h.Facilities == nil {
		return nil
	}
	return h.Facilities.Amenities
}

func TestRoomsAreMapped(t *testing.T) {
	hotel := fetch(t)
	if len(hotel.Rooms) == 0 {
		t.Skip("the captured property published no rooms")
	}

	described := 0
	for _, room := range hotel.Rooms {
		// Amadeus includes placeholder entries carrying only a provider
		// reference (id "ALL") alongside the real room types. Those are its
		// own aggregate rows, not rooms, so at least one real room is the
		// assertion rather than every entry being complete.
		if (room.Name != nil && !room.Name.IsEmpty()) ||
			(room.Description != nil && !room.Description.IsEmpty()) ||
			room.Category != "" || room.Classification != "" {
			described++
		}
		if room.MaxOccupancy != nil && room.MaxOccupancy.Total < 0 {
			t.Errorf("room has negative occupancy: %+v", room.MaxOccupancy)
		}
		// An absent dimensions block must be nil, not a pointer to zeroes.
		if d := room.Dimensions; d != nil && d.Area == 0 && d.Width == 0 && d.Height == 0 && d.Length == 0 {
			t.Error("Dimensions is non-nil but empty; it should have been nil")
		}
	}
	if described == 0 {
		t.Errorf("none of the %d rooms carries identifying information", len(hotel.Rooms))
	}
}

func TestPoliciesAreMapped(t *testing.T) {
	hotel := fetch(t)
	if hotel.Policies == nil {
		t.Skip("the captured property published no policies")
	}

	policies := hotel.Policies
	empty := len(policies.Payment) == 0 && len(policies.CheckInOut) == 0 &&
		len(policies.Cancellation) == 0 && len(policies.Pets) == 0 &&
		len(policies.Tax) == 0 && len(policies.Commission) == 0 &&
		len(policies.Guest) == 0 && len(policies.Loyalty) == 0 &&
		len(policies.StayRequirements) == 0
	if empty {
		t.Error("Policies is non-nil but carries nothing; it should have been nil")
	}

	for _, policy := range policies.CheckInOut {
		if policy.CheckIn == "" && policy.CheckOut == "" &&
			policy.CheckInDescription == nil && policy.CheckOutDescription == nil {
			t.Error("a check-in/out policy carries nothing")
		}
	}
	for _, policy := range policies.Payment {
		if policy.Type == "" && policy.Guarantee == nil && len(policy.Details) == 0 {
			t.Error("a payment policy carries nothing")
		}
	}
}

func TestUnincludedTaxIsDistinguishable(t *testing.T) {
	hotel := fetch(t)
	if hotel.Policies == nil || len(hotel.Policies.Tax) == 0 {
		t.Skip("the captured property published no tax policies")
	}

	for _, tax := range hotel.Policies.Tax {
		if tax.Code == "" && tax.Description == "" {
			t.Error("a tax policy identifies nothing")
		}
		if !tax.Amount.Amount().IsZero() && tax.Amount.Currency() == "" {
			t.Errorf("tax %q has an amount but no currency", tax.Code)
		}
	}
}

func TestNearestDistanceComparesAcrossUnits(t *testing.T) {
	// Amadeus can quote the same landmark in miles and kilometres. Comparing
	// the raw numbers would call 0.4 MILE nearer than 0.5 KM; it is 644m
	// against 500m.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK, `{"data":{
	  "basic":{"hotelId":"HLPAR266","name":"TEST"},
	  "hotel":{"relativeLocation":[{
	    "destination":{"name":"Opera Garnier","subType":"POINT_OF_INTEREST"},
	    "distances":[{"unit":"MILE","value":0.4},{"unit":"KM","value":0.5}]}]}}}`)
	service := content.NewService(server.Client())

	hotel, err := service.Get(context.Background(), content.Query{HotelID: "HLPAR266"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(hotel.NearbyLandmarks) != 1 {
		t.Fatalf("got %d landmarks", len(hotel.NearbyLandmarks))
	}
	landmark := hotel.NearbyLandmarks[0]
	if landmark.Distance == nil {
		t.Fatal("distance was dropped")
	}
	if landmark.Distance.Value != 0.5 || landmark.Distance.Unit != "KM" {
		t.Errorf("nearest = %s, want 0.5 KM (shorter than 0.4 MILE)", landmark.Distance)
	}
}

func TestAwardsAreMapped(t *testing.T) {
	hotel := fetch(t)
	awards := append(append([]content.Award{}, hotel.Awards...), hotel.Certifications...)
	if len(awards) == 0 {
		t.Skip("the captured property published no awards")
	}

	for _, award := range awards {
		if award.Name == "" && award.Provider == "" && award.Rating == "" {
			t.Errorf("award carries nothing: %+v", award)
		}
	}
}

func TestViewDefaultsToFull(t *testing.T) {
	// Amadeus's own default returns the basic block alone, which is almost
	// never what a caller asking for content wants.
	service, server := newService(t)

	if _, err := service.Get(context.Background(), content.Query{HotelID: "HNPARKGU"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := server.LastRequest(t).Query.Get("view"); got != "FULL" {
		t.Errorf("view = %q, want FULL by default", got)
	}
}

func TestQueryParameters(t *testing.T) {
	service, server := newService(t)

	_, err := service.Get(context.Background(), content.Query{
		HotelID: "HNPARKGU",
		View:    codes.ContentViewLight,
		Fields:  []string{"hotel", "rooms"},
		Lang:    "FR",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	query := server.LastRequest(t).Query
	if query.Get("hotelID") != "HNPARKGU" {
		t.Errorf("hotelID = %q", query.Get("hotelID"))
	}
	if query.Get("view") != "LIGHT" {
		t.Errorf("view = %q, an explicit view must not be overridden", query.Get("view"))
	}
	if query.Get("fields") != "hotel,rooms" {
		t.Errorf("fields = %q", query.Get("fields"))
	}
	if query.Get("lang") != "FR" {
		t.Errorf("lang = %q", query.Get("lang"))
	}
}

func TestValidation(t *testing.T) {
	service, server := newService(t)

	cases := []content.Query{
		{},
		{HotelID: "TOOSHORT1234"},
		{HotelID: "HNPARKGU", View: "MEDIUM"},
	}

	before := len(server.Requests())
	for _, query := range cases {
		if _, err := service.Get(context.Background(), query); !errors.Is(err, apierr.ErrValidation) {
			t.Errorf("%+v: err = %v, want ErrValidation", query, err)
		}
	}
	if len(server.Requests()) != before {
		t.Error("an invalid query reached the network")
	}
}

func TestSparseContentDoesNotProduceEmptyStructs(t *testing.T) {
	// Most properties publish far less than the schema allows. An absent block
	// must be nil, so a caller can tell "not published" from "published empty".
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK,
		`{"data":{"basic":{"hotelId":"XXPAR999","name":"SPARSE PROPERTY"}}}`)
	service := content.NewService(server.Client())

	hotel, err := service.Get(context.Background(), content.Query{HotelID: "XXPAR999"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if hotel.Name != "SPARSE PROPERTY" {
		t.Errorf("name = %q", hotel.Name)
	}
	if hotel.Policies != nil {
		t.Errorf("Policies = %+v, want nil when none were published", hotel.Policies)
	}
	if hotel.Facilities != nil {
		t.Errorf("Facilities = %+v, want nil", hotel.Facilities)
	}
	if hotel.Location != nil {
		t.Errorf("Location = %+v, want nil", hotel.Location)
	}
	if hotel.Building != nil || hotel.TimeZone != nil || hotel.Altitude != nil {
		t.Error("absent blocks should stay nil")
	}
	if len(hotel.Rooms) != 0 || len(hotel.Contacts) != 0 || len(hotel.Descriptions) != 0 {
		t.Error("absent lists should stay empty")
	}
	if hotel.Description != nil {
		t.Errorf("Description = %+v, want nil with no prose to draw on", hotel.Description)
	}
	if _, ok := hotel.Position(); ok {
		t.Error("a property with no geoCode has no position")
	}
	if _, ok := hotel.PrimaryPhoto(); ok {
		t.Error("a property with no media has no primary photo")
	}
}

func TestNotFoundSurfacesTyped(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusNotFound,
		`{"errors":[{"status":404,"code":1797,"title":"NOT FOUND"}]}`)
	service := content.NewService(server.Client())

	_, err := service.Get(context.Background(), content.Query{HotelID: "XXPAR999"})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRoomMediaSeparatesPhotographsFromProse(t *testing.T) {
	// Amadeus overloads a room's media array: most entries are photographs,
	// but some are prose blocks with no image at all - a room's default name
	// or category description. Leaving them in Media hands a caller a
	// non-empty slice of assets that render nothing, which reads as "the
	// images are missing" when the real answer is that they were never images.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, contentPath, http.StatusOK, `{"data":{
	  "basic":{"hotelId":"RTPAR666","name":"TEST"},
	  "rooms":[{
	    "name":{"text":"Studio for 2 people"},
	    "media":[
	      {"tags":["DEFAULT_ROOM_NAME"],
	       "description":{"text":"Lounge with twin beds, kitchen.","lang":"en"}},
	      {"id":"IMG1","category":"GUEST_ROOM",
	       "mediaScales":[{"href":"https://example.test/room.jpg",
	                       "dimensions":{"height":600,"width":800}}]}
	    ]}]}}`)

	hotel, err := content.NewService(server.Client()).Get(
		context.Background(), content.Query{HotelID: "RTPAR666"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(hotel.Rooms) != 1 {
		t.Fatalf("got %d rooms", len(hotel.Rooms))
	}
	room := hotel.Rooms[0]

	if len(room.Media) != 1 {
		t.Fatalf("Media = %d entries, want 1 (the photograph only)", len(room.Media))
	}
	if !room.Media[0].IsVisual() {
		t.Error("the entry left in Media carries no image")
	}
	if len(room.Descriptions) != 1 {
		t.Fatalf("Descriptions = %d entries, want 1 (the prose block)", len(room.Descriptions))
	}
	if got := room.Descriptions[0].Value; got != "Lounge with twin beds, kitchen." {
		t.Errorf("Descriptions[0].Value = %q", got)
	}
	// The tag is where Amadeus records what the prose is about, as it does for
	// the property's own description blocks.
	if got := room.Descriptions[0].Type; got != "DEFAULT_ROOM_NAME" {
		t.Errorf("Descriptions[0].Type = %q, want DEFAULT_ROOM_NAME", got)
	}
}
