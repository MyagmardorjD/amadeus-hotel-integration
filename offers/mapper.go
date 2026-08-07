package offers

import (
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto/offersdto"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
)

// The mapper is the anti-corruption layer for this context: the only code that
// sees both Amadeus's wire shapes and the domain.
//
// Mapping never fails. A malformed price or date in one field must not discard
// an otherwise usable offer, so unparseable values become their zero value and
// the rest of the offer survives. The alternative - refusing the whole response
// because one tax line had a bad decimal - loses the caller far more than it
// protects them.

// mapHotelOffers translates a search result.
//
// Rates are response-level rather than per-hotel, so the same map is attached
// to every element; callers reach it wherever they happen to be iterating.
func mapHotelOffers(wire []offersdto.OffersResponse, rates ConversionRates) []HotelOffers {
	if wire == nil {
		return nil
	}
	out := make([]HotelOffers, len(wire))
	for i, h := range wire {
		out[i] = HotelOffers{
			Hotel:     mapHotel(h.Hotel),
			Available: h.Available,
			Offers:    mapOffers(h.Offers),
			Self:      h.Self,
			Rates:     rates,
		}
	}
	return out
}

// mapConversionRates translates the dictionaries block.
//
// A rate whose multiplier cannot be parsed is dropped rather than kept as
// zero: a zero rate would silently convert every price to nothing.
func mapConversionRates(wire map[string]amadeus.ConversionRate) ConversionRates {
	if len(wire) == 0 {
		return nil
	}

	rates := make(ConversionRates, len(wire))
	for from, rate := range wire {
		// ParseRate rather than ParseAmount: Amadeus quotes rates to sixteen
		// decimal places, beyond what money.Amount holds exactly, and refusing
		// a conversion over digits nobody means would be unhelpful.
		parsed, err := money.ParseRate(rate.Rate)
		if err != nil || parsed.IsZero() {
			continue
		}
		rates[money.Currency(from)] = ConversionRate{
			From:          money.Currency(from),
			To:            money.Currency(rate.Target),
			Rate:          parsed,
			DecimalPlaces: rate.TargetDecimalPlaces,
			RawRate:       rate.Rate,
		}
	}

	if len(rates) == 0 {
		return nil
	}
	return rates
}

func mapHotel(h offersdto.HotelResponse) Hotel {
	hotel := Hotel{
		ID:                 h.HotelID,
		Name:               h.Name,
		ChainCode:          h.ChainCode,
		BrandCode:          h.BrandCode,
		DupeID:             h.DupeID,
		CityCode:           h.CityCode,
		Rating:             codes.Rating(h.Rating),
		AmenityCodes:       h.Amenities,
		TermsAndConditions: h.TermsAndConditions,
	}

	// Hotel Search sends latitude and longitude as bare fields rather than a
	// nested object, so "absent" and "0,0" are indistinguishable on the wire.
	// Treating an exact 0,0 as absent is the lesser error: no hotel in this
	// inventory sits in the Gulf of Guinea, and placing one there is worse than
	// omitting its position.
	if h.Latitude != 0 || h.Longitude != 0 {
		hotel.Position = &geo.Coordinates{Latitude: h.Latitude, Longitude: h.Longitude}
	}
	if h.Address != nil {
		hotel.Address = &Address{
			Lines:       h.Address.Lines,
			PostalCode:  h.Address.PostalCode,
			CityName:    h.Address.CityName,
			StateCode:   h.Address.StateCode,
			CountryCode: h.Address.CountryCode,
		}
	}
	if h.Contact != nil {
		hotel.Contact = &Contact{Phone: h.Contact.Phone, Fax: h.Contact.Fax, Email: h.Contact.Email}
	}

	return hotel
}

func mapOffers(wire []offersdto.OfferResponse) []Offer {
	if wire == nil {
		return nil
	}
	out := make([]Offer, len(wire))
	for i, o := range wire {
		out[i] = mapOffer(o)
	}
	return out
}

func mapOffer(o offersdto.OfferResponse) Offer {
	offer := Offer{
		ID: OfferID(o.ID),
		Stay: Stay{
			CheckIn:  parseDate(o.CheckInDate),
			CheckOut: parseDate(o.CheckOutDate),
		},
		Guests:        Guests{Adults: o.Guests.Adults, ChildAges: o.Guests.ChildAges},
		RoomQuantity:  o.RoomQuantity,
		Room:          mapRoom(o.Room),
		Price:         mapPrice(o.Price),
		Policies:      mapPolicies(o.Policies),
		BoardType:     codes.BoardType(o.BoardType),
		RateCode:      codes.RateCode(o.RateCode),
		RateName:      o.RateName,
		IsLoyaltyRate: parseWireBool(o.IsLoyaltyRate),
		Category:      o.Category,
		Extras:        mapExtras(o.Services),
		Self:          o.Self,
	}

	if o.RateFamilyEstimated.Code != "" || o.RateFamilyEstimated.Type != "" {
		offer.RateFamily = &RateFamily{
			Code: o.RateFamilyEstimated.Code,
			Type: o.RateFamilyEstimated.Type,
		}
	}
	if o.Description != nil {
		offer.Description = mapDescription(o.Description)
	}
	if o.RatePromotionCode != nil {
		offer.PromotionCode = &PromotionCode{
			Code:        o.RatePromotionCode.Code,
			Description: o.RatePromotionCode.Description,
		}
	}
	if o.ProviderContentReference != nil {
		offer.ProviderReference = &ProviderReference{
			ID:  o.ProviderContentReference.ID,
			Ref: o.ProviderContentReference.Ref,
		}
	}
	if o.Commission != nil {
		offer.Commission = &Commission{
			Amount:      parseMoney(o.Commission.Amount, o.Price.Currency),
			Percentage:  o.Commission.Percentage,
			Description: mapDescription(o.Commission.Description),
		}
	}
	if o.RoomInformation != nil {
		offer.RoomDetails = mapRoomDetails(*o.RoomInformation, o.Price.Currency)
	}
	if o.StandardizedRoom != nil {
		offer.StandardizedRoom = mapStandardizedRoom(*o.StandardizedRoom)
	}

	return offer
}

func mapRoom(r offersdto.RoomResponse) Room {
	return Room{
		Type:        r.Type,
		Category:    r.TypeEstimated.Category,
		Beds:        r.TypeEstimated.Beds,
		BedType:     r.TypeEstimated.BedType,
		Description: mapDescription(&r.Description),
	}
}

func mapPrice(p offersdto.PriceResponse) Price {
	currency := money.Currency(p.Currency)

	price := Price{
		Currency:        currency,
		Base:            parseMoney(p.Base, p.Currency),
		Total:           parseMoney(p.Total, p.Currency),
		SellingTotal:    parseMoney(p.SellingTotal, p.Currency),
		RateParityTotal: parseMoney(p.RateParityTotal, p.Currency),
		Taxes:           mapTaxes(p.Taxes, p.Currency),
		Markups:         mapMarkups(p.Markups, p.Currency),
		Variations:      mapVariations(p.Variations, p.Currency),
	}

	// Amadeus sends commission in two shapes depending on the source: a legacy
	// nested "commission" array and a flat "commissions" one. Both are folded
	// into one list so callers do not have to know which arrived.
	for _, entry := range p.Commissions {
		price.Commissions = append(price.Commissions, mapCommissionValue(entry.Amount, entry.Percentage, p.Currency))
	}
	for _, legacy := range p.Commission {
		for _, value := range legacy.Values {
			price.Commissions = append(price.Commissions, mapCommissionValue(value.Amount, value.Percentage, p.Currency))
		}
	}

	return price
}

func mapCommissionValue(amount *offersdto.AmountResponse, percentage float64, fallbackCurrency string) CommissionValue {
	value := CommissionValue{Percentage: percentage}
	if amount == nil {
		return value
	}

	currency := amount.Currency
	if currency == "" {
		currency = fallbackCurrency
	}
	value.Amount = parseMoney(amount.Amount, currency)
	value.DecimalPlaces = amount.DecimalPlaces
	value.PriceType = amount.ElementaryPriceType
	value.IssueCurrencyType = amount.IssueCurrencyType
	return value
}

func mapTaxes(wire []offersdto.TaxResponse, fallbackCurrency string) []Tax {
	if wire == nil {
		return nil
	}
	out := make([]Tax, len(wire))
	for i, t := range wire {
		currency := t.Currency
		if currency == "" {
			currency = fallbackCurrency
		}

		out[i] = Tax{
			Amount:               parseMoney(t.Amount, currency),
			Code:                 t.Code,
			Description:          t.Description,
			Percentage:           t.Percentage,
			Included:             t.Included,
			PaidInLoyaltyRewards: t.IsPaidInLoyaltyRewards,
			CollectionPoint:      t.CollectionPoint,
			PricingFrequency:     t.PricingFrequency,
			PricingMode:          t.PricingMode,
		}
		if t.ApplicableDate != nil {
			out[i].Applicable = &DateRange{
				Start: parseDate(t.ApplicableDate.Start),
				End:   parseDate(t.ApplicableDate.End),
			}
		}
	}
	return out
}

func mapMarkups(wire []dto.MarkupResponse, currency string) []money.Money {
	if wire == nil {
		return nil
	}
	out := make([]money.Money, len(wire))
	for i, m := range wire {
		out[i] = parseMoney(m.Amount, currency)
	}
	return out
}

func mapVariations(v offersdto.VariationsResponse, fallbackCurrency string) Variations {
	variations := Variations{
		Average: PricePeriod{
			Currency:     currencyOr(v.Average.Currency, fallbackCurrency),
			Base:         parseMoney(v.Average.Base, orDefault(v.Average.Currency, fallbackCurrency)),
			Total:        parseMoney(v.Average.Total, orDefault(v.Average.Currency, fallbackCurrency)),
			SellingTotal: parseMoney(v.Average.SellingTotal, orDefault(v.Average.Currency, fallbackCurrency)),
			Markups:      mapMarkups(v.Average.Markups, orDefault(v.Average.Currency, fallbackCurrency)),
		},
	}

	for _, change := range v.Changes {
		currency := orDefault(change.Currency, fallbackCurrency)
		variations.Changes = append(variations.Changes, PricePeriod{
			Start:        parseDate(change.StartDate),
			End:          parseDate(change.EndDate),
			Currency:     money.Currency(currency),
			Base:         parseMoney(change.Base, currency),
			Total:        parseMoney(change.Total, currency),
			SellingTotal: parseMoney(change.SellingTotal, currency),
			Markups:      mapMarkups(change.Markups, currency),
		})
	}

	return variations
}

func mapPolicies(p offersdto.PoliciesResponse) Policies {
	policies := Policies{
		PaymentType: p.PaymentType,
	}

	// Amadeus populates either "cancellation" or "cancellations", and
	// sometimes both with the same content. Merging them and dropping the
	// duplicate means callers read one list instead of guessing which field
	// this particular source filled in.
	if !isEmptyCancellation(p.Cancellation) {
		policies.Cancellation = append(policies.Cancellation, mapCancellation(p.Cancellation))
	}
	for _, c := range p.Cancellations {
		if isEmptyCancellation(c) {
			continue
		}
		mapped := mapCancellation(c)
		if len(policies.Cancellation) > 0 && sameCancellation(policies.Cancellation[0], mapped) {
			continue
		}
		policies.Cancellation = append(policies.Cancellation, mapped)
	}

	if p.Refundable != nil {
		policies.Refundable = &RefundPolicy{Status: RefundStatus(p.Refundable.CancellationRefund)}
	}
	if p.Deposit != nil {
		policies.Deposit = mapPaymentPolicy(*p.Deposit)
	}
	if p.Prepay != nil {
		policies.Prepay = mapPaymentPolicy(*p.Prepay)
	}
	if p.Guarantee != nil {
		policies.Guarantee = &GuaranteePolicy{
			Description:      mapDescription(p.Guarantee.Description),
			AcceptedPayments: mapAcceptedPayments(p.Guarantee.AcceptedPayments),
		}
	}
	if p.HoldTime != nil {
		policies.HoldTime = parseTimestamp(p.HoldTime.Deadline)
	}
	if p.CheckInOut != nil {
		policies.CheckInOut = &CheckInOutPolicy{
			CheckIn:             p.CheckInOut.CheckIn,
			CheckOut:            p.CheckInOut.CheckOut,
			CheckInDescription:  mapDescription(p.CheckInOut.CheckInDescription),
			CheckOutDescription: mapDescription(p.CheckInOut.CheckOutDescription),
		}
	}

	policies.LengthOfStay = mapLengthOfStay(p)

	for _, detail := range p.AdditionalDetails {
		for _, description := range detail.Description {
			if text := mapDescription(&description); text != nil {
				policies.Details = append(policies.Details, *text)
			}
		}
	}

	return policies
}

// mapLengthOfStay reconciles the structured block with the two deprecated
// top-level fields Amadeus still sends. Note the wire spelling
// "minimuLengthOfStay", which is Amadeus's typo and is reproduced in the DTO
// because matching it is what makes the field decode.
func mapLengthOfStay(p offersdto.PoliciesResponse) *LengthOfStayPolicy {
	policy := LengthOfStayPolicy{
		Minimum: p.MinimuLengthOfStay,
		Maximum: p.MaximumLengthOfStay,
	}

	if p.LengthOfStay != nil {
		if p.LengthOfStay.MinimumLengthOfStay > 0 {
			policy.Minimum = p.LengthOfStay.MinimumLengthOfStay
		}
		if p.LengthOfStay.MaximumLengthOfStay > 0 {
			policy.Maximum = p.LengthOfStay.MaximumLengthOfStay
		}
		policy.MinimumDescription = mapTextContent(p.LengthOfStay.MinimumLengthOfStayDescription)
		policy.MaximumDescription = mapTextContent(p.LengthOfStay.MaximumLengthOfStayDescription)
	}

	if policy.Minimum == 0 && policy.Maximum == 0 &&
		policy.MinimumDescription == nil && policy.MaximumDescription == nil {
		return nil
	}
	return &policy
}

func mapCancellation(c offersdto.CancellationResponse) CancellationPolicy {
	return CancellationPolicy{
		Amount:         parseMoney(c.Amount, ""),
		Percentage:     c.Percentage,
		NumberOfNights: c.NumberOfNights,
		Deadline:       parseTimestamp(c.Deadline),
		Type:           c.Type,
		PolicyType:     c.PolicyType,
		Description:    mapDescription(&c.Description),
	}
}

func isEmptyCancellation(c offersdto.CancellationResponse) bool {
	return c.Amount == "" && c.Deadline == "" && c.Percentage == "" &&
		c.NumberOfNights == 0 && c.Type == "" && c.PolicyType == "" &&
		c.Description.Text == ""
}

func sameCancellation(a, b CancellationPolicy) bool {
	if a.Percentage != b.Percentage || a.NumberOfNights != b.NumberOfNights ||
		a.Type != b.Type || a.PolicyType != b.PolicyType {
		return false
	}
	if (a.Deadline == nil) != (b.Deadline == nil) {
		return false
	}
	if a.Deadline != nil && !a.Deadline.Equal(*b.Deadline) {
		return false
	}
	return a.Amount.Amount().Equal(b.Amount.Amount())
}

func mapPaymentPolicy(p offersdto.PaymentPolicyResponse) *PaymentPolicy {
	return &PaymentPolicy{
		Amount:           parseMoney(p.Amount, ""),
		Deadline:         parseTimestamp(p.Deadline),
		Description:      mapDescription(p.Description),
		AcceptedPayments: mapAcceptedPayments(p.AcceptedPayments),
	}
}

func mapAcceptedPayments(a *offersdto.AcceptedPaymentsResponse) *AcceptedPayments {
	if a == nil {
		return nil
	}

	accepted := &AcceptedPayments{
		CreditCards: a.CreditCards,
		Methods:     a.Methods,
	}
	for _, policy := range a.CreditCardPolicies {
		card := CreditCardPolicy{VendorCode: policy.VendorCode}
		for _, input := range policy.InputParameters {
			card.Inputs = append(card.Inputs, InputParameter{
				Label:    input.Label,
				Optional: parseWireBool(input.IsOptional),
			})
		}
		accepted.CardPolicies = append(accepted.CardPolicies, card)
	}
	return accepted
}

func mapExtras(wire []offersdto.ServiceResponse) []Extra {
	if wire == nil {
		return nil
	}
	out := make([]Extra, len(wire))
	for i, s := range wire {
		out[i] = Extra{
			Code:          s.Code,
			Description:   s.Description,
			IsChargeable:  s.IsChargeable,
			PricingMethod: s.PricingMethod,
			Quantity:      s.Quantity,
			Attribute:     s.ServiceAttribute,
			Price:         mapExtraPrice(s.Price),
		}
	}
	return out
}

func mapExtraPrice(p *offersdto.ServicePriceResponse) *ExtraPrice {
	if p == nil {
		return nil
	}

	price := &ExtraPrice{
		Currency:     money.Currency(p.Currency),
		Base:         parseMoney(p.Base, p.Currency),
		Total:        parseMoney(p.Total, p.Currency),
		SellingTotal: parseMoney(p.SellingTotal, p.Currency),
		Taxes:        mapTaxes(p.Taxes, p.Currency),
		Markups:      mapMarkups(p.Markups, p.Currency),
	}
	if p.Variations != nil {
		variations := mapVariations(*p.Variations, p.Currency)
		price.Variations = &variations
	}
	return price
}

func mapRoomDetails(r offersdto.RoomInformationResponse, currency string) *RoomDetails {
	details := &RoomDetails{
		ID:               r.ID,
		Name:             mapTextContent(r.Name),
		Type:             r.Type,
		Description:      r.Description,
		Category:         r.HotelRoomCategory,
		Classification:   r.HotelRoomClassification,
		Location:         r.HotelRoomLocation,
		Architecture:     r.ArchitectureCode,
		ViewCode:         r.ViewCode,
		Beds:             r.Beds,
		BedType:          r.BedType,
		BedroomsPerRoom:  r.BedroomsPerRoom,
		BathroomsPerRoom: r.BathroomsPerRoom,
		Quantity:         r.Quantity,
		SortOrder:        r.SortOrder,
		Dimensions:       mapDimensions(r.Dimensions),
		Media:            mapMediaAssets(r.Media),
	}

	if r.ProviderContentReference != nil {
		details.ProviderReference = &ProviderReference{
			ID:  r.ProviderContentReference.ID,
			Ref: r.ProviderContentReference.Ref,
		}
	}

	// The estimated block is the fallback for the flat fields, which some
	// sources leave empty.
	if r.TypeEstimated != nil {
		if details.Beds == 0 {
			details.Beds = r.TypeEstimated.Beds
		}
		if details.BedType == "" {
			details.BedType = r.TypeEstimated.BedType
		}
		if details.Category == "" {
			details.Category = r.TypeEstimated.Category
		}
	}
	if r.MaxPersonCapacity != nil {
		details.MaxOccupancy = &Occupancy{
			Adults:   r.MaxPersonCapacity.Adults,
			Children: r.MaxPersonCapacity.Children,
			Total:    r.MaxPersonCapacity.Total,
		}
	}
	if r.MaxSleepFurnishings != nil {
		details.SleepFurnishings = &SleepFurnishings{
			Cribs:     r.MaxSleepFurnishings.Cribs,
			ExtraBeds: r.MaxSleepFurnishings.ExtraBeds,
		}
	}
	for _, amenity := range r.Amenities {
		details.Amenities = append(details.Amenities, mapRoomAmenity(amenity, currency))
	}
	for i := range r.PolicyDescriptions {
		if text := mapDescription(&r.PolicyDescriptions[i]); text != nil {
			details.PolicyDescriptions = append(details.PolicyDescriptions, *text)
		}
	}

	return details
}

func mapRoomAmenity(a offersdto.RoomAmenityResponse, currency string) RoomAmenity {
	amenity := RoomAmenity{
		Code:                  a.Code,
		Description:           a.Description,
		Type:                  a.AmenityType,
		Attribute:             a.AmenityAttribute,
		QualityAssessment:     a.AmenityQualityAssessment,
		PerformanceAssessment: a.AmenityPerformanceAssessment,
		PricingMethod:         a.PricingMethod,
		Quantity:              a.Quantity,
		Price:                 mapExtraPrice(a.Price),
		Media:                 mapMediaAssets(a.Medias),
	}
	if a.AmenityProvider != nil {
		amenity.Provider = a.AmenityProvider.Name
	}
	return amenity
}

func mapStandardizedRoom(r offersdto.StandardizedRoomResponse) *StandardizedRoom {
	room := &StandardizedRoom{
		ID:         r.ID,
		Name:       r.Name,
		Dimensions: mapDimensions(r.Dimensions),
	}

	if r.MaxPersonCapacity != nil {
		room.MaxOccupancy = &Occupancy{
			Adults:   r.MaxPersonCapacity.Adults,
			Children: r.MaxPersonCapacity.Children,
			Total:    r.MaxPersonCapacity.Total,
		}
	}
	for _, amenity := range r.Amenities {
		room.Amenities = append(room.Amenities, StandardizedAmenity{
			Code:        amenity.Code,
			Description: amenity.Description,
		})
	}
	for _, view := range r.Views {
		room.Views = append(room.Views, StandardizedView{
			Code:        view.Code,
			Description: view.Description,
		})
	}
	for _, config := range r.BedConfigurations {
		for _, item := range config.BedConfigurationItem {
			room.BedConfigurations = append(room.BedConfigurations, BedConfiguration{
				Beds:       item.Beds,
				Attributes: item.Bed,
			})
		}
	}

	return room
}
