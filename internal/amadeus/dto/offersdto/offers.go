package offersdto

import "github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto"

// Package offersdto holds the wire structures for the Hotel Search API (v3.5): GET /shopping/hotel-offers
// (v3.5): GET /shopping/hotel-offers and GET /shopping/hotel-offers/{offerId}.
//
// Two Amadeus quirks are reproduced faithfully here and normalised by the
// mapper rather than by the struct tags:
//   - isLoyaltyRate and isOptional arrive as quoted strings ("true"/"false"),
//     not JSON booleans.
//   - Every monetary value is a decimal string, with the currency held on a
//     parent object rather than beside the amount.

type (
	// OffersResponse models a single item of the GET /shopping/hotel-offers
	// 200 response `data` array (a hotel with its bookable offers).
	OffersResponse struct {
		// Always "hotel-offers".
		Type      string          `json:"type"`
		Hotel     HotelResponse   `json:"hotel"`
		Available bool            `json:"available"`
		Offers    []OfferResponse `json:"offers"`
		Self      string          `json:"self"`
	}

	// HotelOffersResponse is the top-level GET /shopping/hotel-offers 200
	// response wrapper containing the data array plus meta/dictionaries/warnings.
	HotelOffersResponse struct {
		Data         []OffersResponse      `json:"data"`
		Meta         *MetaResponse         `json:"meta,omitempty"`
		Dictionaries *DictionariesResponse `json:"dictionaries,omitempty"`
		Warnings     []WarningResponse     `json:"warnings,omitempty"`
	}

	// MetaResponse holds response-level metadata.
	MetaResponse struct {
		Links *MetaLinksResponse `json:"links,omitempty"`
	}

	// MetaLinksResponse holds pagination links.
	MetaLinksResponse struct {
		// URL to the next page of results.
		Next string `json:"next,omitempty"`
	}

	// DictionariesResponse holds lookup dictionaries referenced by the response.
	DictionariesResponse struct {
		CurrencyConversionLookupRates map[string]CurrencyConversionRateResponse `json:"currencyConversionLookupRates,omitempty"`
	}

	// CurrencyConversionRateResponse describes a currency conversion lookup rate.
	CurrencyConversionRateResponse struct {
		Rate                string `json:"rate,omitempty"`
		Target              string `json:"target,omitempty"`
		TargetDecimalPlaces int    `json:"targetDecimalPlaces,omitempty"`
	}

	// WarningResponse models a non-blocking warning returned with the response.
	WarningResponse struct {
		Code          int                         `json:"code,omitempty"`
		Title         string                      `json:"title,omitempty"`
		Detail        string                      `json:"detail,omitempty"`
		Source        *dto.WarningSourceResponse  `json:"source,omitempty"`
		Documentation string                      `json:"documentation,omitempty"`
		Sources       []dto.WarningSourceResponse `json:"sources,omitempty"`
	}

	HotelResponse struct {
		Type      string `json:"type"`
		HotelID   string `json:"hotelId"`
		ChainCode string `json:"chainCode"`
		// Brand code of the hotel.
		BrandCode string `json:"brandCode,omitempty"`
		DupeID    string `json:"dupeId"`
		Name      string `json:"name"`
		// Hotel star rating. Enum: 5, 4, 3, 2, 1.
		Rating    string  `json:"rating,omitempty"`
		CityCode  string  `json:"cityCode"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		// Link to the terms and conditions the guest must approve to book.
		TermsAndConditions string `json:"termsAndConditions,omitempty"`
		// Contact details of the hotel.
		Contact *HotelContactResponse `json:"contact,omitempty"`
		// Postal address of the hotel.
		Address *HotelAddressResponse `json:"address,omitempty"`
		// Amenity codes offered by the hotel.
		Amenities []string `json:"amenities,omitempty"`
	}

	// HotelContactResponse models the hotel contact details returned with an offer.
	HotelContactResponse struct {
		Phone string `json:"phone,omitempty"`
		Fax   string `json:"fax,omitempty"`
		Email string `json:"email,omitempty"`
	}

	// HotelAddressResponse models the hotel postal address returned with an offer.
	HotelAddressResponse struct {
		Lines       []string `json:"lines,omitempty"`
		PostalCode  string   `json:"postalCode,omitempty"`
		CityName    string   `json:"cityName,omitempty"`
		CountryCode string   `json:"countryCode,omitempty"`
		StateCode   string   `json:"stateCode,omitempty"`
	}

	OfferResponse struct {
		ID string `json:"id"`
		// Always "hotel-offer".
		Type string `json:"type,omitempty"`
		// Board type. Enum: ROOM_ONLY, BREAKFAST, HALF_BOARD, ALL_INCLUSIVE,
		// BUFFET_BREAKFAST, CARIBBEAN_BREAKFAST, CONTINENTAL_BREAKFAST,
		// ENGLISH_BREAKFAST, FULL_BREAKFAST, LUNCH, DINNER, FAMILY,
		// AS_BROCHURED, SELF_CATERING, BERMUDA, FULL_BOARD, FAMILY_AMERICAN,
		// MODIFIED, BREAKFAST_AND_LUNCH, LUNCH_AND_DINNER.
		BoardType string `json:"boardType,omitempty"`
		// Category of the offer.
		Category     string `json:"category,omitempty"`
		CheckInDate  string `json:"checkInDate"`
		CheckOutDate string `json:"checkOutDate"`
		// Commission information for the offer.
		Commission *CommissionResponse `json:"commission,omitempty"`
		// Free-text description of the offer.
		Description *dto.DescriptionResponse `json:"description,omitempty"`
		Guests      GuestsResponse           `json:"guests"`
		// Whether this is a loyalty rate.
		// Amadeus returns this as a quoted string ("true"/"false"), not a JSON bool.
		IsLoyaltyRate       string                          `json:"isLoyaltyRate,omitempty"`
		RateCode            string                          `json:"rateCode"`
		RateFamilyEstimated dto.RateFamilyEstimatedResponse `json:"rateFamilyEstimated"`
		// Marketing name of the rate.
		RateName string `json:"rateName,omitempty"`
		// Promotion code applied to the rate.
		RatePromotionCode *RatePromotionCodeResponse `json:"ratePromotionCode,omitempty"`
		Room              RoomResponse               `json:"room"`
		// Detailed standardized room information.
		RoomInformation *RoomInformationResponse `json:"roomInformation,omitempty"`
		// Reference to the content provider's data.
		ProviderContentReference *ProviderContentReferenceResponse `json:"providerContentReference,omitempty"`
		// Number of rooms booked under this offer.
		RoomQuantity int              `json:"roomQuantity,omitempty"`
		Price        PriceResponse    `json:"price"`
		Policies     PoliciesResponse `json:"policies"`
		// Additional chargeable or complimentary services.
		Services []ServiceResponse `json:"services,omitempty"`
		// Standardized (normalized) room description.
		StandardizedRoom *StandardizedRoomResponse `json:"standardizedRoom,omitempty"`
		Self             string                    `json:"self"`
	}

	// CommissionResponse describes commission paid to the booker.
	CommissionResponse struct {
		// Flat commission amount.
		Amount string `json:"amount,omitempty"`
		// Description of the commission.
		Description *dto.DescriptionResponse `json:"description,omitempty"`
		// Commission percentage.
		Percentage string `json:"percentage,omitempty"`
	}

	// RatePromotionCodeResponse describes a rate promotion code.
	RatePromotionCodeResponse struct {
		Code        string `json:"code,omitempty"`
		Description string `json:"description,omitempty"`
	}

	// ProviderContentReferenceResponse references content provider data.
	ProviderContentReferenceResponse struct {
		ID  string `json:"id,omitempty"`
		Ref string `json:"ref,omitempty"`
	}

	RoomResponse struct {
		Type          string                  `json:"type"`
		TypeEstimated TypeEstimatedResponse   `json:"typeEstimated"`
		Description   dto.DescriptionResponse `json:"description"`
	}

	TypeEstimatedResponse struct {
		// Room category, e.g. STANDARD_ROOM, SUITE, etc.
		Category string `json:"category"`
		Beds     int    `json:"beds"`
		// Bed type. Enum: DOUBLE, KING, QUEEN, TWIN, SINGLE, PULLOUT, WATER_BED.
		BedType string `json:"bedType"`
	}

	GuestsResponse struct {
		Adults    int   `json:"adults"`
		ChildAges []int `json:"childAges"`
	}

	PriceResponse struct {
		Currency string `json:"currency"`
		Base     string `json:"base"`
		// Per-room commission breakdown (legacy format).
		Commission []PriceCommissionResponse `json:"commission,omitempty"`
		// Commission breakdown (current format).
		Commissions []PriceCommissionEntryResponse `json:"commissions,omitempty"`
		// Markups applied to the price.
		Markups []dto.MarkupResponse `json:"markups,omitempty"`
		// Total before rate parity adjustments.
		RateParityTotal string `json:"rateParityTotal,omitempty"`
		// Selling total including markups.
		SellingTotal string `json:"sellingTotal,omitempty"`
		// Taxes applicable to the price.
		Taxes      []TaxResponse      `json:"taxes,omitempty"`
		Total      string             `json:"total"`
		Variations VariationsResponse `json:"variations"`
	}

	// PriceCommissionResponse models the legacy commission format on price.
	PriceCommissionResponse struct {
		Values []PriceCommissionValueResponse `json:"values,omitempty"`
	}

	// PriceCommissionValueResponse holds an amount/percentage commission value.
	PriceCommissionValueResponse struct {
		Amount     *AmountResponse `json:"amount,omitempty"`
		Percentage float64         `json:"percentage,omitempty"`
	}

	// PriceCommissionEntryResponse models the current commission format.
	PriceCommissionEntryResponse struct {
		Amount     *AmountResponse `json:"amount,omitempty"`
		Percentage float64         `json:"percentage,omitempty"`
	}

	// AmountResponse is a structured monetary amount.
	AmountResponse struct {
		Amount              string `json:"amount,omitempty"`
		Currency            string `json:"currency,omitempty"`
		DecimalPlaces       int    `json:"decimalPlaces,omitempty"`
		ElementaryPriceType string `json:"elementaryPriceType,omitempty"`
		IssueCurrencyType   string `json:"issueCurrencyType,omitempty"`
		Value               int    `json:"value,omitempty"`
	}

	// TaxResponse models a tax line item.
	TaxResponse struct {
		Amount      string `json:"amount,omitempty"`
		Code        string `json:"code,omitempty"`
		Currency    string `json:"currency,omitempty"`
		Description string `json:"description,omitempty"`
		Included    bool   `json:"included,omitempty"`
		// Whether the tax is paid in loyalty rewards.
		IsPaidInLoyaltyRewards bool   `json:"isPaidInLoyaltyRewards,omitempty"`
		Percentage             string `json:"percentage,omitempty"`
		// Date range over which the tax applies.
		ApplicableDate   *ApplicableDateResponse `json:"applicableDate,omitempty"`
		PricingFrequency string                  `json:"pricingFrequency,omitempty"`
		PricingMode      string                  `json:"pricingMode,omitempty"`
		// Collection point. Enum: AT_HOTEL_PROPERTY, AT_BOOKING_TIME.
		CollectionPoint string `json:"collectionPoint,omitempty"`
	}

	// ApplicableDateResponse models a start/end date-time range.
	ApplicableDateResponse struct {
		Start string `json:"start,omitempty"`
		End   string `json:"end,omitempty"`
	}

	VariationsResponse struct {
		Average AverageResponse  `json:"average"`
		Changes []ChangeResponse `json:"changes"`
	}

	AverageResponse struct {
		Base string `json:"base"`
		// Currency of the average price.
		Currency string `json:"currency,omitempty"`
		// Markups applied to the average price.
		Markups []dto.MarkupResponse `json:"markups,omitempty"`
		// Selling total for the average price.
		SellingTotal string `json:"sellingTotal,omitempty"`
		// Total for the average price.
		Total string `json:"total,omitempty"`
	}

	ChangeResponse struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		// Base price for the period.
		Base string `json:"base,omitempty"`
		// Currency for the period.
		Currency string `json:"currency,omitempty"`
		// Markups applied for the period.
		Markups []dto.MarkupResponse `json:"markups,omitempty"`
		// Selling total for the period.
		SellingTotal string `json:"sellingTotal,omitempty"`
		Total        string `json:"total"`
	}

	PoliciesResponse struct {
		PaymentType string `json:"paymentType"`
		// Additional policy details.
		AdditionalDetails []PolicyDetailResponse `json:"additionalDetails,omitempty"`
		Cancellation      CancellationResponse   `json:"cancellation"`
		// List of cancellation policies.
		Cancellations []CancellationResponse `json:"cancellations,omitempty"`
		// Check-in / check-out timing policy.
		CheckInOut *CheckInOutResponse `json:"checkInOut,omitempty"`
		// Deposit policy.
		Deposit *PaymentPolicyResponse `json:"deposit,omitempty"`
		// Guarantee policy.
		Guarantee *GuaranteePolicyResponse `json:"guarantee,omitempty"`
		// Hold-time policy.
		HoldTime *HoldTimeResponse `json:"holdTime,omitempty"`
		// Length-of-stay policy.
		LengthOfStay *LengthOfStayResponse `json:"lengthOfStay,omitempty"`
		// Maximum length of stay (deprecated top-level field).
		MaximumLengthOfStay int `json:"maximumLengthOfStay,omitempty"`
		// Minimum length of stay (deprecated top-level field; swagger spelling).
		MinimuLengthOfStay int `json:"minimuLengthOfStay,omitempty"`
		// Prepay policy.
		Prepay *PaymentPolicyResponse `json:"prepay,omitempty"`
		// Refundability of the offer.
		Refundable *RefundableResponse `json:"refundable,omitempty"`
	}

	// PolicyDetailResponse holds additional policy description text.
	PolicyDetailResponse struct {
		Description []dto.DescriptionResponse `json:"description,omitempty"`
	}

	CancellationResponse struct {
		// Cancellation fee amount.
		Amount string `json:"amount,omitempty"`
		// Cancellation deadline (date-time).
		Deadline    string                  `json:"deadline,omitempty"`
		Description dto.DescriptionResponse `json:"description"`
		// Number of nights charged on cancellation.
		NumberOfNights int `json:"numberOfNights,omitempty"`
		// Cancellation fee percentage.
		Percentage string `json:"percentage,omitempty"`
		// Policy type. Enum: CANCELLATION, EARLY_CHECKOUT, NO_SHOW.
		PolicyType string `json:"policyType,omitempty"`
		// Type. Enum: FULL_STAY.
		Type string `json:"type"`
	}

	// CheckInOutResponse models check-in/check-out timing policy.
	CheckInOutResponse struct {
		CheckIn             string                   `json:"checkIn,omitempty"`
		CheckInDescription  *dto.DescriptionResponse `json:"checkInDescription,omitempty"`
		CheckOut            string                   `json:"checkOut,omitempty"`
		CheckOutDescription *dto.DescriptionResponse `json:"checkOutDescription,omitempty"`
	}

	// PaymentPolicyResponse models deposit/prepay payment policies.
	PaymentPolicyResponse struct {
		AcceptedPayments *AcceptedPaymentsResponse `json:"acceptedPayments,omitempty"`
		Amount           string                    `json:"amount,omitempty"`
		// Payment deadline (date-time).
		Deadline    string                   `json:"deadline,omitempty"`
		Description *dto.DescriptionResponse `json:"description,omitempty"`
	}

	// GuaranteePolicyResponse models a guarantee payment policy.
	GuaranteePolicyResponse struct {
		AcceptedPayments *AcceptedPaymentsResponse `json:"acceptedPayments,omitempty"`
		Description      *dto.DescriptionResponse  `json:"description,omitempty"`
	}

	// AcceptedPaymentsResponse lists accepted payment methods.
	AcceptedPaymentsResponse struct {
		CreditCardPolicies []CreditCardPolicyResponse `json:"creditCardPolicies,omitempty"`
		// Accepted credit card vendor codes.
		CreditCards []string `json:"creditCards,omitempty"`
		// Accepted payment methods.
		Methods []string `json:"methods,omitempty"`
	}

	// CreditCardPolicyResponse models per-vendor credit card policy.
	CreditCardPolicyResponse struct {
		InputParameters []InputParameterResponse `json:"inputParameters,omitempty"`
		VendorCode      string                   `json:"vendorCode,omitempty"`
	}

	// InputParameterResponse describes one field the guest must supply for a
	// credit card payment (e.g. card holder name), and whether it is optional.
	InputParameterResponse struct {
		Label string `json:"label,omitempty"`
		// Amadeus returns this as a quoted string ("true"/"false"), not a JSON bool.
		IsOptional string `json:"isOptional,omitempty"`
	}

	// HoldTimeResponse models a hold-time policy.
	HoldTimeResponse struct {
		// Hold deadline (date-time).
		Deadline string `json:"deadline,omitempty"`
	}

	// LengthOfStayResponse models length-of-stay constraints.
	LengthOfStayResponse struct {
		MaximumLengthOfStay            int                      `json:"maximumLengthOfStay,omitempty"`
		MaximumLengthOfStayDescription *dto.TextContentResponse `json:"maximumLengthOfStayDescription,omitempty"`
		MinimumLengthOfStay            int                      `json:"minimumLengthOfStay,omitempty"`
		MinimumLengthOfStayDescription *dto.TextContentResponse `json:"minimumLengthOfStayDescription,omitempty"`
	}

	// RefundableResponse models refundability.
	RefundableResponse struct {
		// Refund type. Enum: NON_REFUNDABLE, REFUNDABLE,
		// REFUNDABLE_UP_TO_DEADLINE, UNKNOWN.
		CancellationRefund string `json:"cancellationRefund,omitempty"`
	}

	// ServiceResponse models an additional chargeable/complimentary service.
	ServiceResponse struct {
		Code         string                `json:"code,omitempty"`
		Description  string                `json:"description,omitempty"`
		IsChargeable bool                  `json:"isChargeable,omitempty"`
		Price        *ServicePriceResponse `json:"price,omitempty"`
		// Pricing method. Enum: DAILY, HOURLY, HALF_DAY, PER_OCCURRENCE,
		// PER_EVENT, PER_PERSON, FIRST_USE, PER_MINUTE, COMPLIMENTARY, WEEKLY,
		// PER_STAY, PER_FUNCTION, PER_ROOM_PER_STAY, PER_ROOM_PER_NIGHT,
		// PER_PERSON_PER_STAY, PER_PERSON_PER_NIGHT, PER_RESERVATION_BOOKNG,
		// PER_USE.
		PricingMethod    string `json:"pricingMethod,omitempty"`
		Quantity         int    `json:"quantity,omitempty"`
		ServiceAttribute string `json:"serviceAttribute,omitempty"`
	}

	// ServicePriceResponse models the price of a service or amenity.
	ServicePriceResponse struct {
		Base         string               `json:"base,omitempty"`
		Currency     string               `json:"currency,omitempty"`
		Markups      []dto.MarkupResponse `json:"markups,omitempty"`
		SellingTotal string               `json:"sellingTotal,omitempty"`
		Taxes        []TaxResponse        `json:"taxes,omitempty"`
		Total        string               `json:"total,omitempty"`
		Variations   *VariationsResponse  `json:"variations,omitempty"`
	}

	// RoomInformationResponse models the detailed room information block.
	RoomInformationResponse struct {
		// Room amenities with pricing and media.
		Amenities []RoomAmenityResponse `json:"amenities,omitempty"`
		// Architecture style. Enum: ART_DECO, BRAZILIAN, CONTEMPORARY,
		// HIGH_RISE, HISTORIC, MEDITERRANEAN, MODERN, ORIENTAL, SOUTHWEST,
		// TRADITIONAL, VICTORIAN, WESTERN, ANCIENT, THEMED.
		ArchitectureCode string `json:"architectureCode,omitempty"`
		BathroomsPerRoom int    `json:"bathroomsPerRoom,omitempty"`
		// Bed type. Enum: DOUBLE, FUTON, KING, MURPHY_BED, QUEEN, SOFA_BED,
		// TATAMI_MATS, TWIN, SINGLE, FULL, RUN_OF_THE_HOUSE, DORM_BED, WATER_BED.
		BedType         string                  `json:"bedType,omitempty"`
		BedroomsPerRoom int                     `json:"bedroomsPerRoom,omitempty"`
		Beds            int                     `json:"beds,omitempty"`
		Description     string                  `json:"description,omitempty"`
		Dimensions      *dto.DimensionsResponse `json:"dimensions,omitempty"`
		// Room category. Enum includes SUITE, BUDGET, DELUXE, ECONOMY, LUXURY,
		// STANDARD, MIDSCALE, UPSCALE, etc.
		HotelRoomCategory string `json:"hotelRoomCategory,omitempty"`
		// Room classification. Enum includes ROOM, VILLA, APARTMENTS, SUITES,
		// STUDIOS, etc.
		HotelRoomClassification string                           `json:"hotelRoomClassification,omitempty"`
		HotelRoomLocation       string                           `json:"hotelRoomLocation,omitempty"`
		ID                      string                           `json:"id,omitempty"`
		MaxPersonCapacity       *dto.MaxPersonCapacityResponse   `json:"maxPersonCapacity,omitempty"`
		MaxSleepFurnishings     *dto.MaxSleepFurnishingsResponse `json:"maxSleepFurnishings,omitempty"`
		Media                   []dto.MediaResponse              `json:"media,omitempty"`
		Name                    *dto.TextContentResponse         `json:"name,omitempty"`
		PolicyDescriptions      []dto.DescriptionResponse        `json:"policyDescriptions,omitempty"`
		// Reference to the content provider's own record for this room, which
		// is a different reference from the offer-level one.
		ProviderContentReference *ProviderContentReferenceResponse     `json:"providerContentReference,omitempty"`
		Quantity                 int                                   `json:"quantity,omitempty"`
		SortOrder                int                                   `json:"sortOrder,omitempty"`
		Type                     string                                `json:"type,omitempty"`
		TypeEstimated            *RoomInformationTypeEstimatedResponse `json:"typeEstimated,omitempty"`
		// View code. Enum includes CITY, OCEAN, POOL, MOUNTAIN, GARDEN, SEA, etc.
		ViewCode string `json:"viewCode,omitempty"`
	}

	// RoomAmenityResponse models a room amenity.
	RoomAmenityResponse struct {
		// Media associated with the amenity.
		Medias                       []dto.MediaResponse   `json:"Medias,omitempty"`
		AmenityAttribute             string                `json:"amenityAttribute,omitempty"`
		AmenityPerformanceAssessment string                `json:"amenityPerformanceAssessment,omitempty"`
		AmenityProvider              *dto.AmenityProvider  `json:"amenityProvider,omitempty"`
		AmenityQualityAssessment     string                `json:"amenityQualityAssessment,omitempty"`
		AmenityType                  string                `json:"amenityType,omitempty"`
		Code                         string                `json:"code,omitempty"`
		Description                  string                `json:"description,omitempty"`
		Price                        *ServicePriceResponse `json:"price,omitempty"`
		// Pricing method. Enum: DAILY, HOURLY, HALF_DAY, PER_OCCURRENCE,
		// PER_EVENT, PER_PERSON, FIRST_USE, PER_MINUTE, COMPLIMENTARY, WEEKLY,
		// PER_STAY, PER_FUNCTION, PER_ROOM_PER_STAY, PER_ROOM_PER_NIGHT,
		// PER_PERSON_PER_STAY, PER_PERSON_PER_NIGHT, PER_RESERVATION_BOOKNG.
		PricingMethod string `json:"pricingMethod,omitempty"`
		Quantity      int    `json:"quantity,omitempty"`
	}

	// RoomInformationTypeEstimatedResponse models the estimated room type.
	RoomInformationTypeEstimatedResponse struct {
		// Bed type. Enum: DOUBLE, KING, QUEEN, TWIN, SINGLE, PULLOUT, WATER_BED.
		BedType string `json:"bedType,omitempty"`
		Beds    int    `json:"beds,omitempty"`
		// Category. Enum includes STANDARD_ROOM, SUITE, STUDIO, VILLA, etc.
		Category string `json:"category,omitempty"`
	}

	// StandardizedRoomResponse models normalized room data.
	StandardizedRoomResponse struct {
		Amenities         []StandardizedAmenityResponse  `json:"amenities,omitempty"`
		BedConfigurations []BedConfigurationResponse     `json:"bedConfigurations,omitempty"`
		Dimensions        *dto.DimensionsResponse        `json:"dimensions,omitempty"`
		ID                string                         `json:"id,omitempty"`
		MaxPersonCapacity *dto.MaxPersonCapacityResponse `json:"maxPersonCapacity,omitempty"`
		Name              string                         `json:"name,omitempty"`
		Views             []StandardizedViewResponse     `json:"views,omitempty"`
	}

	// StandardizedAmenityResponse models a normalized amenity.
	StandardizedAmenityResponse struct {
		Code        string `json:"code,omitempty"`
		Description string `json:"description,omitempty"`
	}

	// BedConfigurationResponse models a bed configuration.
	BedConfigurationResponse struct {
		BedConfigurationItem []BedConfigurationItemResponse `json:"bedConfigurationItem,omitempty"`
	}

	// BedConfigurationItemResponse models a single bed configuration item.
	BedConfigurationItemResponse struct {
		Bed  map[string]any `json:"bed,omitempty"`
		Beds string         `json:"beds,omitempty"`
	}

	// StandardizedViewResponse models a normalized room view.
	StandardizedViewResponse struct {
		Code        string `json:"code,omitempty"`
		Description string `json:"description,omitempty"`
	}

	DetailResponse struct {
		Message    string            `json:"message"`
		Parameters map[string]string `json:"parameters"`
	}
)
