package content

import (
	"strings"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto/contentdto"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/mapping"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/media"
)

// The anti-corruption layer for the content context.
//
// Content is the schema with the most optional blocks: what a property
// publishes varies enormously by source. Every helper here returns a nil
// pointer or an empty slice for an absent block rather than a zero-valued
// struct, so callers can tell "the property published no policies" from "the
// property published empty policies".

// mapHotel translates the whole content response into the domain aggregate.
func mapHotel(c contentdto.HotelContentResponse) Hotel {
	basic := c.Basic

	hotel := Hotel{
		ID:              basic.HotelID,
		Name:            basic.Name,
		ChainCode:       basic.ChainCode,
		BrandCode:       basic.BrandCode,
		ChainName:       basic.ChainName,
		BrandName:       basic.BrandName,
		DupeID:          basic.DupeID,
		Rating:          codes.Rating(basic.Rating),
		Status:          string(basic.Status),
		Description:     mapping.QualifiedText(&basic.Description),
		Amenities:       mapAmenities(basic.Amenities),
		Categories:      basic.Category,
		CategoryCode:    string(basic.CategoryCode),
		DefaultLanguage: basic.DefaultSpokenLanguage,
		TaxID:           c.Hotel.TaxID,
		Currencies:      c.Hotel.CurrencyCode,
		SpokenLanguages: c.Hotel.SpokenLanguages,
		Climate:         c.Hotel.Climate,
		Awards:          mapAwards(c.Awards),
		Certifications:  mapAwards(c.Hotel.Certifications),
		Promotions:      mapPromotions(c.Promotions),
		Rooms:           mapRooms(c.Rooms),
	}

	// Amadeus mixes photographs and prose in one media array, and populates
	// basic.description only rarely. Splitting them here is what stops the
	// property's own description being lost among the images.
	hotel.Media, hotel.Descriptions = splitMedia(basic.Media)
	if hotel.Description == nil {
		hotel.Description = longestDescription(hotel.Descriptions)
	}

	if location := mapLocation(basic.Location); location != nil {
		hotel.Location = location
	}
	for _, segment := range basic.Segment {
		hotel.Segments = append(hotel.Segments, string(segment))
	}
	for _, area := range basic.Area {
		hotel.Areas = append(hotel.Areas, Area{Type: string(area.HotelAreaType), Name: area.Name})
	}
	for _, contact := range basic.Contact {
		if mapped := mapContact(contact); mapped != nil {
			hotel.Contacts = append(hotel.Contacts, *mapped)
		}
	}
	for _, identifier := range basic.HotelBusinessIdentifications.Identifiers {
		hotel.BusinessIdentifiers = append(hotel.BusinessIdentifiers,
			Identifier{ID: identifier.ID, Name: identifier.Name})
	}
	for _, period := range basic.Season.OpenCalendar {
		hotel.OpenPeriods = append(hotel.OpenPeriods, mapPeriod(period))
	}
	if basic.Altitude.Value != 0 || basic.Altitude.Unit != "" {
		hotel.Altitude = &Altitude{Value: basic.Altitude.Value, Unit: string(basic.Altitude.Unit)}
	}

	hotel.TimeZone = mapTimeZone(c.Hotel.TimeZone)
	hotel.Building = mapBuilding(c.Hotel.Building)
	hotel.Facilities = mapFacilities(c.Facilities)
	hotel.Policies = mapPolicies(c.Policies)

	for _, poi := range c.PointOfInterest {
		hotel.PointsOfInterest = append(hotel.PointsOfInterest, mapPointOfInterest(poi))
	}
	for _, relative := range c.Hotel.RelativeLocation {
		hotel.NearbyLandmarks = append(hotel.NearbyLandmarks, mapLandmark(relative))
	}

	return hotel
}

// descriptionPreference ranks the tags Amadeus uses for prose, best first. The
// long property description is what a listing page wants; a location blurb or a
// marketing line is a poor substitute but better than nothing.
var descriptionPreference = []string{
	"HOTEL_LONG_DESCRIPTION",
	"PROPERTY_DESCRIPTION",
	"HOTEL_SHORT_DESCRIPTION",
	"LONG_LOCATION_DESCRIPTION",
	"SHORT_LOCATION_DESCRIPTION",
	"MARKETING",
}

// splitMedia separates the photographs from the prose blocks that share the
// media array, tagging each prose block with what it describes.
func splitMedia(wire []dto.MediaResponse) (photos []media.Asset, prose []media.Text) {
	for _, asset := range mapping.MediaAssets(wire) {
		if asset.IsVisual() {
			photos = append(photos, asset)
			continue
		}
		if asset.Description == nil || asset.Description.IsEmpty() {
			// Neither an image nor text: nothing to keep.
			continue
		}

		text := *asset.Description
		if text.Type == "" && len(asset.Tags) > 0 {
			// The tag is where Amadeus records what the prose is about.
			text.Type = asset.Tags[0]
		}
		prose = append(prose, text)
	}
	return photos, prose
}

// longestDescription picks the best available prose block to serve as the
// property's description, preferring the tags a listing page would want and
// falling back to the longest text when none of them are present.
func longestDescription(prose []media.Text) *media.Text {
	for _, preferred := range descriptionPreference {
		for i, text := range prose {
			if text.Type == preferred {
				return &prose[i]
			}
		}
	}

	best := -1
	for i, text := range prose {
		if best == -1 || len(text.Value) > len(prose[best].Value) {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	return &prose[best]
}

func mapAmenities(wire []dto.AmenityResponse) []Amenity {
	if wire == nil {
		return nil
	}
	out := make([]Amenity, len(wire))
	for i, a := range wire {
		out[i] = Amenity{
			Code:                  a.Code,
			Description:           a.Description,
			Type:                  a.AmenityType,
			Attribute:             a.AmenityAttribute,
			QualityAssessment:     a.AmenityQualityAssessment,
			PerformanceAssessment: a.AmenityPerformanceAssessment,
			IsChargeable:          a.IsChargeable,
			PricingMethod:         string(a.PricingMethod),
			Quantity:              a.Quantity,
			Media:                 mapping.MediaAssets(a.Media),
		}
		if a.AmenityProvider != nil {
			out[i].Provider = a.AmenityProvider.Name
		}
	}
	return out
}

func mapLocation(l contentdto.LocationResponse) *Location {
	subType := l.SubType
	if subType == "" {
		// Amadeus spells this field two ways across its schemas, and populates
		// whichever the source used.
		subType = l.Subtype
	}

	location := &Location{
		Name:     l.Name,
		SubType:  subType,
		IATACode: l.IataCode,
	}
	if l.GeoCode.Latitude != 0 || l.GeoCode.Longitude != 0 {
		location.Position = &geo.Coordinates{
			Latitude:  l.GeoCode.Latitude,
			Longitude: l.GeoCode.Longitude,
		}
	}

	if location.Name == "" && location.IATACode == "" && location.Position == nil {
		return nil
	}
	return location
}

func mapContact(c contentdto.ContactResponse) *Contact {
	contact := &Contact{
		Purposes:     c.Purpose,
		LocationType: string(c.LocationType),
		Website:      firstNonEmpty(c.Website.Url, c.Website.Href),
	}

	if name := strings.TrimSpace(strings.Join([]string{
		c.AddresseeName.Prefix, c.AddresseeName.FirstName,
		c.AddresseeName.MiddleName, c.AddresseeName.LastName,
	}, " ")); name != "" {
		contact.AddresseeName = strings.Join(strings.Fields(name), " ")
	}

	if address := mapAddress(c.Address); address != nil {
		contact.Address = address
	}
	if c.Phones.Number != "" {
		contact.Phones = append(contact.Phones, Phone{
			Number:   fullNumber(c.Phones),
			Category: string(c.Phones.Category),
			Type:     string(c.Phones.DeviceType),
		})
	}
	if email := firstNonEmpty(c.Email.Email, c.Email.Address); email != "" {
		contact.Emails = append(contact.Emails, email)
	}

	if contact.Address == nil && len(contact.Phones) == 0 &&
		len(contact.Emails) == 0 && contact.Website == "" {
		return nil
	}
	return contact
}

// fullNumber assembles the dialable number from the parts Amadeus splits it
// into. The parts are frequently populated while the bare Number is not, so
// joining them is what makes the number usable.
func fullNumber(p contentdto.PhoneResponse) string {
	var b strings.Builder
	if p.CountryCallingCode != "" {
		b.WriteString("+")
		b.WriteString(p.CountryCallingCode)
	}
	if p.AreaCode != "" {
		b.WriteString(p.AreaCode)
	}
	b.WriteString(p.Number)
	if p.Extension != "" {
		b.WriteString(" ext. ")
		b.WriteString(p.Extension)
	}
	return b.String()
}

func mapAddress(a contentdto.AddressResponse) *Address {
	address := &Address{
		Lines:       a.Lines,
		PostalCode:  a.PostalCode,
		CityName:    a.CityName,
		StateCode:   firstNonEmpty(a.StateCode, a.State),
		CountryCode: a.CountryCode,
		CountryName: a.CountryName,
		PostalBox:   a.PostalBox,
	}
	if len(address.Lines) == 0 && address.CityName == "" &&
		address.PostalCode == "" && address.CountryCode == "" {
		return nil
	}
	return address
}

func mapPeriod(p contentdto.Period) Period {
	period := Period{}
	if p.Start != nil {
		period.Start = mapping.Date(p.Start.Format("2006-01-02"))
	}
	if p.End != nil {
		period.End = mapping.Date(p.End.Format("2006-01-02"))
	}
	return period
}

func mapTimeZone(t contentdto.TimeZoneResponse) *TimeZone {
	if t.Name == "" && t.ID == "" && t.OffSet == "" {
		return nil
	}
	return &TimeZone{
		Name:           firstNonEmpty(t.Name, t.ID, t.Code),
		OffsetHours:    t.OffSet,
		DaylightSaving: t.DstOffsetInSeconds != 0,
	}
}

func mapBuilding(b contentdto.BuildingResponse) *Building {
	if b.NumberOfFloors == 0 && b.NumberOfRooms == 0 &&
		b.BuiltDate == "" && b.RenovationDate == "" {
		return nil
	}
	return &Building{
		Floors:        b.NumberOfFloors,
		TotalRooms:    b.NumberOfRooms,
		YearBuilt:     b.BuiltDate,
		YearRenovated: b.RenovationDate,
		Description:   string(b.ArchitectureCode),
	}
}

func mapRooms(wire []contentdto.RoomResponse) []Room {
	if wire == nil {
		return nil
	}
	out := make([]Room, len(wire))
	for i, r := range wire {
		out[i] = Room{
			Name:               mapping.QualifiedText(&r.Name),
			Description:        mapping.QualifiedText(&r.Description),
			Classification:     string(r.HotelRoomClassification),
			Category:           string(r.HotelRoomCategory),
			Location:           r.HotelRoomLocation,
			Architecture:       string(r.ArchitectureCode),
			ViewCode:           string(r.ViewCode),
			Beds:               r.Beds,
			BedType:            string(r.BedType),
			Bedrooms:           r.BedRoomsPerRoom,
			Bathrooms:          r.BathroomsPerRoom,
			Quantity:           r.Quantity,
			SortOrder:          r.SortOrder,
			IsNonSmoking:       r.IsNonSmoking,
			StandardOccupancy:  r.StandardPersonCapacity,
			Amenities:          mapAmenities(r.Amenities),
			PolicyDescriptions: r.PolicyDescriptions,
		}

		// A room's media array carries prose blocks beside its photographs,
		// exactly as the property's does, so it is split the same way.
		out[i].Media, out[i].Descriptions = splitMedia(r.Media)

		// The wire sends dimensions as an embedded struct rather than a
		// pointer, so taking its address unconditionally would hand callers a
		// non-nil pointer to zeroes - and `if room.Dimensions != nil` would
		// always be true, telling them nothing.
		if d := r.Dimensions; d.Area != 0 || d.Width != 0 || d.Height != 0 || d.Length != 0 {
			out[i].Dimensions = mapping.Dimensions(&d)
		}

		if capacity := r.MaxPersonCapacity; capacity.Total != 0 || capacity.Adults != 0 {
			out[i].MaxOccupancy = &Occupancy{
				Adults:   capacity.Adults,
				Children: capacity.Children,
				Total:    capacity.Total,
			}
		}
		if furnishings := r.MaxSleepFurnishings; furnishings.Cribs != 0 || furnishings.ExtraBeds != 0 {
			out[i].SleepFurnishings = &SleepFurnishings{
				Cribs:     furnishings.Cribs,
				ExtraBeds: furnishings.ExtraBeds,
			}
		}
		if r.ProviderContentReference.ID != "" {
			out[i].ProviderReference = &ProviderReference{ID: r.ProviderContentReference.ID}
		}
	}
	return out
}

func mapAwards(wire []contentdto.AwardsResponse) []Award {
	if wire == nil {
		return nil
	}
	out := make([]Award, len(wire))
	for i, a := range wire {
		out[i] = Award{
			Name:         a.Name,
			Provider:     a.ProviderName,
			Rating:       a.Rating,
			RatingSystem: string(a.RatingSystem),
			Description:  a.Description,
			DateGranted:  a.DateGranted,
		}
	}
	return out
}

func mapPromotions(wire []contentdto.PromotionResponse) []Promotion {
	if wire == nil {
		return nil
	}
	out := make([]Promotion, len(wire))
	for i, p := range wire {
		out[i] = Promotion{
			Name:               p.Name,
			Description:        p.Description,
			Category:           string(p.Category),
			Code:               p.Code,
			TermsAndConditions: p.TermsAndConditions.Text,
			Media:              mapping.MediaAssets(p.Media),
		}
	}
	return out
}

func mapPointOfInterest(p contentdto.PointOfInterestResponse) PointOfInterest {
	poi := PointOfInterest{
		Name:         p.Basic.Name,
		Description:  p.Description,
		CategoryCode: string(p.CategoryCode),
		Location:     mapLocation(p.Location),
		Website:      p.OfficialWebsite.Url,
		Media:        mapping.MediaAssets(p.Media),
	}

	if poi.Name == "" && poi.Location != nil {
		poi.Name = poi.Location.Name
	}
	if contact := mapContact(p.Contact); contact != nil {
		poi.Contact = contact
	}
	if p.Season.Start != nil || p.Season.End != nil {
		season := mapPeriod(p.Season)
		poi.Season = &season
	}
	if distance := nearestDistance(p.LocationDistance); distance != nil {
		poi.Distance = distance
	}

	return poi
}

func mapLandmark(l contentdto.LocationDistanceResponse) Landmark {
	landmark := Landmark{
		Name:     l.Destination.Name,
		Type:     firstNonEmpty(l.Destination.SubType, l.Destination.Subtype),
		Distance: nearestDistance(l),
	}
	return landmark
}

// nearestDistance picks one distance from the several Amadeus can publish for a
// place, preferring the shortest. It measures them in meters first so a value
// quoted in miles is not treated as smaller than one in kilometers.
func nearestDistance(l contentdto.LocationDistanceResponse) *geo.Distance {
	var best *geo.Distance
	for _, d := range l.Distances {
		if d.Value == 0 {
			continue
		}
		candidate := geo.NewDistance(d.Value, geo.Unit(d.Unit))
		if best == nil || candidate.Meters() < best.Meters() {
			distance := candidate
			best = &distance
		}
	}
	return best
}

func mapFacilities(f contentdto.FacilityResponse) *Facilities {
	facilities := &Facilities{Amenities: mapAmenities(f.Amenities)}

	if info := f.MeetingRoomInfo; info.Quantity > 0 || len(info.MeetingRooms) > 0 {
		rooms := &MeetingRooms{
			// len() when Amadeus states no quantity, which is the common case: it
			// publishes the rooms themselves and leaves the counter at zero, and a
			// property with one named meeting room used to report having none.
			Count:           max(info.Quantity, len(info.MeetingRooms)),
			LargestCapacity: info.LargestRoomSeatOccupancy,
		}
		if area := info.LargestRoomSpace; area.Area != 0 {
			rooms.TotalArea = &media.Dimensions{
				Area:          area.Area,
				AreaUnit:      string(area.AreaUnit),
				Width:         area.Width,
				Height:        area.Height,
				Length:        area.Length,
				Unit:          string(area.Unit),
				DecimalPlaces: area.DecimalPlaces,
			}
		}
		for _, room := range info.MeetingRooms {
			rooms.Rooms = append(rooms.Rooms, mapMeetingRoom(room))
		}
		for _, room := range rooms.Rooms {
			if room.Description != "" {
				rooms.Description = room.Description
				break
			}
		}
		facilities.MeetingRooms = rooms
	}

	if info := f.RestaurantInfo; info.Quantity > 0 || len(info.Restaurants) > 0 {
		restaurants := &Restaurants{Count: max(info.Quantity, len(info.Restaurants))}
		seen := make(map[string]bool)
		for _, restaurant := range info.Restaurants {
			venue := mapRestaurant(restaurant)
			for _, cuisine := range venue.Cuisines {
				if !seen[cuisine] {
					seen[cuisine] = true
					restaurants.Cuisines = append(restaurants.Cuisines, cuisine)
				}
			}
			if restaurants.Description == "" {
				restaurants.Description = venue.Description
			}
			restaurants.Venues = append(restaurants.Venues, venue)
		}
		facilities.Restaurants = restaurants
	}

	if len(facilities.Amenities) == 0 &&
		facilities.MeetingRooms == nil && facilities.Restaurants == nil {
		return nil
	}
	return facilities
}

// mapMeetingRoom translates one named event space.
func mapMeetingRoom(wire contentdto.MeetingRoomResponse) MeetingRoom {
	photos, prose := splitMedia(wire.Media)
	room := MeetingRoom{
		Name:        wire.Name,
		Type:        string(wire.MeetingRoomType),
		Description: venueDescription(wire.Description, prose),
		Dimensions:  roomDimensions(wire.RoomDimensions),
		Media:       photos,
		SortOrder:   wire.SortOrder,
	}
	for _, layout := range wire.OccupancyPerLayouts {
		room.Capacities = append(room.Capacities, LayoutCapacity{
			Layout:       string(layout.Layout),
			MaxOccupancy: layout.MaxOccupancy,
		})
	}
	return room
}

// mapRestaurant translates one named dining venue.
func mapRestaurant(wire contentdto.RestaurantResponse) Restaurant {
	photos, prose := splitMedia(wire.Media)
	venue := Restaurant{
		Name:            wire.Name,
		Category:        string(wire.Category),
		Description:     venueDescription(wire.Description, prose),
		MaxSeating:      int(wire.MaxSeatingCapacity),
		ServesBreakfast: wire.HasBreakfast,
		ServesBrunch:    wire.HasBrunch,
		ServesLunch:     wire.HasLunch,
		ServesDinner:    wire.HasDinner,
		Media:           photos,
	}
	for _, cuisine := range wire.CuisineTypes {
		if cuisine != "" {
			venue.Cuisines = append(venue.Cuisines, cuisine)
		}
	}
	return venue
}

// venueDescription is the prose for a venue.
//
// Amadeus files it in one of two places and, on the properties measured, only
// ever uses the second: the venue's own `description` is empty and the text sits
// in a prose entry of its media array, alongside the photographs. Reading the
// field alone therefore returned nothing for a venue that plainly had prose.
func venueDescription(stated string, prose []media.Text) string {
	if stated != "" {
		return stated
	}
	for _, text := range prose {
		if text.Value != "" {
			return text.Value
		}
	}
	return ""
}

// roomDimensions translates a room's measurements, nil when it has none.
func roomDimensions(wire contentdto.RoomDimensionsResponse) *media.Dimensions {
	if wire.Area == 0 && wire.Width == 0 && wire.Height == 0 && wire.Length == 0 {
		return nil
	}
	return &media.Dimensions{
		Area:          wire.Area,
		AreaUnit:      string(wire.AreaUnit),
		Width:         wire.Width,
		Height:        wire.Height,
		Length:        wire.Length,
		Unit:          string(wire.Unit),
		DecimalPlaces: wire.DecimalPlaces,
	}
}

func mapPolicies(p contentdto.PolicyResponse) *Policies {
	policies := &Policies{}

	for _, payment := range p.PaymentPolicies {
		mapped := PaymentPolicy{Type: string(payment.PaymentType)}
		for i := range payment.AdditionalDetails {
			if text := mapping.QualifiedText(&payment.AdditionalDetails[i]); text != nil {
				mapped.Details = append(mapped.Details, *text)
			}
		}
		guarantee := payment.Guarantee
		if len(guarantee.AcceptedPayments.CreditCards) > 0 ||
			len(guarantee.AcceptedPayments.Methods) > 0 ||
			guarantee.Description.Text != "" {
			mapped.Guarantee = &Guarantee{
				AcceptedCards:   guarantee.AcceptedPayments.CreditCards,
				AcceptedMethods: stringsOf(guarantee.AcceptedPayments.Methods),
				Description:     mapping.QualifiedText(&guarantee.Description),
			}
		}
		policies.Payment = append(policies.Payment, mapped)
	}

	for _, policy := range p.CheckInOutPolicies {
		policies.CheckInOut = append(policies.CheckInOut, CheckInOutPolicy{
			CheckIn:             policy.CheckIn,
			CheckOut:            policy.CheckOut,
			CheckInDescription:  mapping.QualifiedText(&policy.CheckInDescription),
			CheckOutDescription: mapping.QualifiedText(&policy.CheckOutDescription),
		})
	}

	for _, policy := range p.CancellationPolicies {
		policies.Cancellation = append(policies.Cancellation, CancellationPolicy{
			Amount:         mapping.Money(policy.Amount, ""),
			Percentage:     policy.Percentage,
			NumberOfNights: policy.NumberOfNights,
			Deadline:       policy.Deadline,
			PolicyType:     policy.PolicyType,
			Description:    mapping.QualifiedText(&policy.Description),
		})
	}

	for _, policy := range p.PetsPolicies {
		policies.Pets = append(policies.Pets, PetPolicy{
			Code:          policy.Code,
			Description:   policy.Description,
			PricingMethod: string(policy.PricingMethod),
		})
	}

	for _, policy := range p.TaxPolicies {
		policies.Tax = append(policies.Tax, TaxPolicy{
			Code:        policy.Code,
			Description: policy.Description,
			Amount:      mapping.Money(policy.Amount, policy.Currency),
			Percentage:  policy.Percentage,
			Included:    policy.Included,
			Frequency:   policy.PricingFrequency,
			Mode:        policy.PricingMode,
		})
	}

	for _, policy := range p.CommissionPolicies {
		policies.Commission = append(policies.Commission, CommissionPolicy{
			Percentage:  policy.Percentage,
			Amount:      mapping.Money(policy.Amount, ""),
			Description: mapping.QualifiedText(&policy.Description),
		})
	}

	for _, policy := range p.GuestPolicies {
		policies.Guest = append(policies.Guest, GuestPolicy{
			MinimumGuestAge:          policy.MinGuestAge,
			MaxChildAgeForBedSharing: policy.MaxChildAgeforBedSharing,
			ChildStayFree:            policy.ChildStayFree,
			ChildStayFreeCutoffAge:   policy.ChildStayFreeCutoffAge,
		})
	}

	for _, policy := range p.LoyaltyPolicies {
		mapped := LoyaltyPolicy{Eligibility: policy.Eligibility}
		for _, benefit := range policy.BenefitsAccruals {
			mapped.Benefits = append(mapped.Benefits, LoyaltyBenefit{
				Type:        benefit.LoyaltyAwardType,
				Description: firstNonEmpty(benefit.CodeDescription, benefit.Category),
				Unit:        benefit.Code,
			})
		}
		if policy.Discount.Percentage != "" {
			mapped.Discount = &LoyaltyDiscount{Percentage: policy.Discount.Percentage}
		}
		policies.Loyalty = append(policies.Loyalty, mapped)
	}

	for i := range p.StayRequirements {
		if text := mapping.QualifiedText(&p.StayRequirements[i]); text != nil {
			policies.StayRequirements = append(policies.StayRequirements, *text)
		}
	}

	if len(policies.Payment) == 0 && len(policies.CheckInOut) == 0 &&
		len(policies.Cancellation) == 0 && len(policies.Pets) == 0 &&
		len(policies.Tax) == 0 && len(policies.Commission) == 0 &&
		len(policies.Guest) == 0 && len(policies.Loyalty) == 0 &&
		len(policies.StayRequirements) == 0 {
		return nil
	}
	return policies
}

// firstNonEmpty returns the first non-empty value, which is the shape of
// Amadeus publishing the same fact under two different field names.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// stringsOf converts a slice of any string-kinded type to plain strings.
func stringsOf[T ~string](values []T) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
