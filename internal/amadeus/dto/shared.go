package dto

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Shared wire structures used by more than one Amadeus hotel API. These are
// identical across the search, booking and content schemas, so they are defined
// once here rather than per endpoint.
//
// Names keep the Response suffix they carry in the Amadeus schemas. Inside this
// package that reads as dto.MediaResponse, which is unambiguous against the
// domain types (content.Media, offers.Price) it maps onto.

type QualifiedFreeTextType string

const (
	PropertyDescription               QualifiedFreeTextType = "PROPERTY_DESCRIPTION"
	AmenityInformation                QualifiedFreeTextType = "AMENITY_INFORMATION"
	PromotionalInformation            QualifiedFreeTextType = "PROMOTIONAL_INFORMATION"
	Dining                            QualifiedFreeTextType = "DINING"
	GeneralMeetingPlanningInformation QualifiedFreeTextType = "GENERAL_MEETING_PLANNING_INFORMATION"
	Services                          QualifiedFreeTextType = "SERVICES"
	Marketing                         QualifiedFreeTextType = "MARKETING"
	TypicalDescription                QualifiedFreeTextType = "TYPICAL_DESCRIPTION"
	SellMarketing                     QualifiedFreeTextType = "SELL_MARKETING"
	TopSellingFeature                 QualifiedFreeTextType = "TOP_SELLING_FEATURE"
	AreasServed                       QualifiedFreeTextType = "AREAS_SERVED"
	CategoryDescription               QualifiedFreeTextType = "CATEGORY_DESCRIPTION"
	OtherDescription                  QualifiedFreeTextType = "OTHER_DESCRIPTION"
	HotelShortDescription             QualifiedFreeTextType = "HOTEL_SHORT_DESCRIPTION"
	HotelLongDescription              QualifiedFreeTextType = "HOTEL_LONG_DESCRIPTION"
	LongLocationDescription           QualifiedFreeTextType = "LONG_LOCATION_DESCRIPTION"
	ShortLocationDescription          QualifiedFreeTextType = "SHORT_LOCATION_DESCRIPTION"
	DefaultRoomTypeDescription        QualifiedFreeTextType = "DEFAULT_ROOM_TYPE_DESCRIPTION"
	MeetingFacilitiesDescription      QualifiedFreeTextType = "MEETING_FACILITIES_DESCRIPTION"
	GroupMeetingDescription           QualifiedFreeTextType = "GROUP_MEETING_DESCRIPTION"
	FacilityDescription               QualifiedFreeTextType = "FACILITY_DESCRIPTION"
	OnsiteFacilities                  QualifiedFreeTextType = "ONSITE_FACILITIES"
	OffsiteFacilities                 QualifiedFreeTextType = "OFFSITE_FACILITIES"
	OnsiteServices                    QualifiedFreeTextType = "ONSITE_SERVICES"
	OffsiteServices                   QualifiedFreeTextType = "OFFSITE_SERVICES"
	OnsiteRecreationalActivities      QualifiedFreeTextType = "ONSITE_RECREATIONAL_ACTIVITIES"
	OffsiteRecreationalActivities     QualifiedFreeTextType = "OFFSITE_RECREATIONAL_ACTIVITIES"
	SecurityInformation               QualifiedFreeTextType = "SECURITY_INFORMATION"
	AdditionalOccupant                QualifiedFreeTextType = "ADDITIONAL_OCCUPANT"
	RateDisclaimer                    QualifiedFreeTextType = "RATE_DISCLAIMER"
	TaxAndFeeDescription              QualifiedFreeTextType = "TAX_AND_FEE_DESCRIPTION"
	GeneralPolicyDescription          QualifiedFreeTextType = "GENERAL_POLICY_DECRIPTION"
	CommissionPolicyDescription       QualifiedFreeTextType = "COMMISSION_POLICY_DESCRIPTION"
	CommissionException               QualifiedFreeTextType = "COMMISSION_EXCEPTION"
	VisaTravelRequirements            QualifiedFreeTextType = "VISA_TRAVEL_REQUIREMENTS"
	ExtraChargesDescription           QualifiedFreeTextType = "EXTRA_CHARGES_DESCRIPTION"
	ExtendedStayDescription           QualifiedFreeTextType = "EXTENDED_STAY_DESCRIPTION"
	BookingPolicyDescription          QualifiedFreeTextType = "BOOKING_POLICY_DESCRIPTION"
	ServiceChargeDescription          QualifiedFreeTextType = "SERVICE_CHARGE_DESCRIPTION"
	GroupConditions                   QualifiedFreeTextType = "GROUP_CONDITIONS"
	EarlyCheckoutDescription          QualifiedFreeTextType = "EARLY_CHECKOUT_DESCRIPTION"
	LateCheckoutDescription           QualifiedFreeTextType = "LATE_CHECKOUT_DESCRIPTION"
	LastRoomDescription               QualifiedFreeTextType = "LAST_ROOM_DESCRIPTION"
	RoomTypeGuaranteed                QualifiedFreeTextType = "ROOM_TYPE_GUARANTEED"
	DiningDescription                 QualifiedFreeTextType = "DINING_DECRIPTION"
	HotelRoomDescription              QualifiedFreeTextType = "HOTEL_ROOM_DESCRIPTION"
	RoomAmenityDescription            QualifiedFreeTextType = "ROOM_AMENITY_DESCRIPTION"
	StandardRoomCategory              QualifiedFreeTextType = "STANDARD_ROOM_CATEGORY"
	RoomCategoryName                  QualifiedFreeTextType = "ROOM_CATEGORY_NAME"
	DefaultRoomName                   QualifiedFreeTextType = "DEFAULT_ROOM_NAME"
	RoomCategory                      QualifiedFreeTextType = "ROOM_CATEGORY"
	RestaurantImages                  QualifiedFreeTextType = "RESTAURANT_IMAGES"
	SpecialOffersDescription          QualifiedFreeTextType = "SPECIAL_OFFERS_DESCRIPTION"
	CateringDescription               QualifiedFreeTextType = "CATERING_DESCRIPTION"
	CuisineDescription                QualifiedFreeTextType = "CUISINE_DESCRIPTION"
	RestaurantService                 QualifiedFreeTextType = "RESTAURANT_SERVICE"
	TransportationDescription         QualifiedFreeTextType = "TRANSPORTATION_DESCRIPTION"
	CheckoutInstructions              QualifiedFreeTextType = "CHECKOUT_INSTRUCTIONS"
	DamageDeposit                     QualifiedFreeTextType = "DAMAGE_DEPOSIT"
)

type (
	MediaResponse struct {
		// Id - unique media identifier. Example: "69810B23CB8644A18AF760DC66BE41A6".
		Id string `json:"id,omitempty"`
		// Type - media data type. Enum: file, Image, Icon.
		Type string `json:"type,omitempty"`
		// Name - name of the media file. Example: "guest_room".
		Name string `json:"name,omitempty"`
		// Title - media title. Example: "My image title".
		Title string `json:"title,omitempty"`
		// Caption - media caption text.
		Caption string `json:"caption,omitempty"`
		// Hint - additional hint for the media.
		Hint string `json:"hint,omitempty"`
		// Alt - media description for visually impaired (screen reader text). See W3C WAI guidelines.
		Alt string `json:"alt,omitempty"`
		// Href - URL to display the original media.
		Href string `json:"href,omitempty"`
		// Description - free text description of the media with language info.
		Description *QualifiedFreeTextResponse `json:"description,omitempty"`
		// Category - media category. Example: "EXTERIOR".
		Category string `json:"category,omitempty"`
		// Tags - tags associated with the media.
		Tags []string `json:"tags,omitempty"`
		// MediaType - MIME type of the media. Example: "IMAGE".
		MediaType string `json:"mediaType,omitempty"`
		// MediaScales - scaled versions of the media with different sizes and dimensions.
		MediaScales []MediaScaleResponse `json:"mediaScales,omitempty"`
		// MediaMetaData - metadata about the media (encoding, dimensions, source, etc.).
		MediaMetaData *MediaMetaDataResponse `json:"mediaMetaData,omitempty"`
	}

	// MediaScale represents a scaled version of media with different size and dimension.
	MediaScaleResponse struct {
		// Href - URL to display the scaled version of the media.
		Href string `json:"href,omitempty"`
		// Size - file size of the scaled media.
		Size *MediaSizeResponse `json:"size,omitempty"`
		// Dimensions - physical dimensions (width, height, etc.) of the scaled media.
		Dimensions *DimensionsResponse `json:"dimensions,omitempty"`
		// Duration - duration of the media per ISO 8601. Example: "P1Y2M3DT4H5M6S".
		Duration string `json:"duration,omitempty"`
	}

	// MediaSize represents the size of a media file.
	MediaSizeResponse struct {
		// Unit - unit type for the size value.
		// Enum: NIGHT, PIXELS, KILOGRAMS, POUNDS, CENTIMETERS, INCHES, BYTES, KILOBYTES, etc.
		Unit string `json:"unit,omitempty"`
		// Value - numeric size value. Example: 200.
		Value int `json:"value,omitempty"`
	}

	// Dimensions represents measurements (width, height, length, area) of a media or object.
	DimensionsResponse struct {
		// Area - total surface area. Example: 445.
		Area float64 `json:"area,omitempty"`
		// AreaUnit - unit for area measurement.
		// Enum: SQUARE_FEET, SQUARE_METERS, SQUARE_INCHES, SQUARE_YARDS, etc.
		AreaUnit string `json:"areaUnit,omitempty"`
		// DecimalPlaces - number of decimal places for values.
		DecimalPlaces int `json:"decimalPlaces,omitempty"`
		// Height - height of the object in specified unit.
		Height int `json:"height,omitempty"`
		// Length - length of the object in specified unit.
		Length int `json:"length,omitempty"`
		// Unit - unit type for height/width/length.
		// Enum: PIXELS, CENTIMETERS, INCHES, etc.
		Unit string `json:"unit,omitempty"`
		// Width - width of the object in specified unit.
		Width int `json:"width,omitempty"`
	}

	// MediaMetaData contains metadata about a media file (type, encoding, source, etc.).
	MediaMetaDataResponse struct {
		// MediaType - IANA media type. Enum: application, audio, font, example, image, message, model, multipart, text, video.
		MediaType string `json:"mediaType,omitempty"`
		// SubType - media subtype / file format. Example: "PNG", "MKV".
		SubType string `json:"subType,omitempty"`
		// Encoding - media encoding format. Example: "PNG", "H265".
		Encoding string `json:"encoding,omitempty"`
		// Etag - date and time of the last update in ISO 8601 format.
		Etag string `json:"etag,omitempty"`
		// Size - file size of the media.
		Size *MediaSizeResponse `json:"size,omitempty"`
		// Dimensions - physical dimensions of the media.
		Dimensions *DimensionsResponse `json:"dimensions,omitempty"`
		// Duration - duration per ISO 8601. Example: "P1Y2M3DT4H5M6S".
		Duration string `json:"duration,omitempty"`
		// Application - application name for viewing or editing the media.
		Application string `json:"application,omitempty"`
		// MediaSource - source and copyright information of the media owner.
		MediaSource *MediaSourceResponse `json:"mediaSource,omitempty"`
		// ClickToAction - hyperlink action associated with the media.
		ClickToAction *ClickToActionResponse `json:"clickToAction,omitempty"`
	}

	// MediaSource contains source and copyright information for the media owner.
	MediaSourceResponse struct {
		// Code - owner code of the media.
		Code string `json:"code,omitempty"`
		// Copyright - copyright text related to the media owner.
		Copyright string `json:"copyright,omitempty"`
		// Filename - file name of the media.
		Filename string `json:"filename,omitempty"`
		// Symbology - logo or icon designation.
		Symbology string `json:"symbology,omitempty"`
		// Version - version of the file.
		Version string `json:"version,omitempty"`
	}

	ClickToActionResponse struct {
		// PlainText - hyperlink text content.
		PlainText string `json:"plainText,omitempty"`
		// Href - URL associated with the action text.
		Href string `json:"href,omitempty"`
	}

	// * Specific type to convey a list of string for specific information type ( via qualifier) in specific character set, or language
	QualifiedFreeTextResponse struct {
		Text            string                `json:"text"`            // example: Text of the qualified free text
		Type            QualifiedFreeTextType `json:"type"`            // example: PROPERTY_DESCRIPTION. Type of the qualified free text
		Lang            string                `json:"lang"`            // example: fr-FR. Language of the qualified free text
		Status          string                `json:"status"`          // example: ACTIVE. Status of the qualified free text
		CharSet         string                `json:"charSet"`         // example: UTF-8. Character set of the qualified free text
		Encoding        string                `json:"encoding"`        // example: Base-64. Encoding of the qualified free text
		IamaContentType string                `json:"iamaContentType"` // example: text/plain Follow the RFC define by http://www.iana.org/assignments/media-types/media-types.xhtml
	}
)

type (
	AmenityResponse struct {
		// Code - unique code representing the amenity.
		Code string `json:"code,omitempty"`
		// Description - description of the amenity.
		Description string `json:"description,omitempty"`
		// AmenityType - type/category of the amenity. Example: "WIFI", "Internet".
		AmenityType string `json:"amenityType,omitempty"`
		// AmenityAttribute - attribute related to the amenity type. Example: "USB Outlet" for Power amenity.
		AmenityAttribute string `json:"amenityAttribute,omitempty"`
		// AmenityQualityAssessment - ranking indicator for the amenity. Example: "Standard".
		AmenityQualityAssessment string `json:"amenityQualityAssessment,omitempty"`
		// AmenityPerformanceAssessment - performance indicator. Example: "Low", "High" (e.g. Wifi bandwidth).
		AmenityPerformanceAssessment string `json:"amenityPerformanceAssessment,omitempty"`
		// AmenityProvider - source of the amenity content. Example: ATPCO.
		AmenityProvider *AmenityProvider `json:"amenityProvider,omitempty"`
		// IsChargeable - true if usage of the amenity is chargeable. Default: false.
		IsChargeable bool `json:"isChargeable,omitempty"`
		// Price - price information for using the amenity.
		Price *AmenityPrice `json:"price,omitempty"`
		// PricingMethod - how the amenity usage cost is assessed.
		// Enum: DAILY, HOURLY, HALF_DAY, PER_OCCURRENCE, PER_EVENT, PER_PERSON, FIRST_USE,
		// PER_MINUTE, COMPLIMENTARY, WEEKLY, PER_STAY, PER_FUNCTION, PER_ROOM_PER_STAY,
		// PER_ROOM_PER_NIGHT, PER_PERSON_PER_STAY, PER_PERSON_PER_NIGHT, PER_RESERVATION_BOOKNG.
		PricingMethod PricingMethod `json:"pricingMethod,omitempty"`
		// Quantity - how many counts are available for this amenity type.
		Quantity int `json:"quantity,omitempty"`
		// Media - list of media (images/videos) associated with the amenity.
		Media []MediaResponse `json:"media,omitempty"`
	}

	// AmenityProvider contains the source of the amenity content.
	AmenityProvider struct {
		// Name - name of the amenity content source. Example: "ATPCO".
		Name string `json:"name,omitempty"`
	}

	// AmenityPrice contains price information for an amenity.
	AmenityPrice struct {
		// Base - base price of the amenity.
		Base string `json:"base,omitempty"`
		// Currency - currency code applied to the price.
		Currency string `json:"currency,omitempty"`
		// Markups - markups applied to the amenity price.
		Markups []MarkupResponse `json:"markups,omitempty"`
		// SellingTotal - selling total = total + margins + markup + totalFees - discounts.
		SellingTotal string `json:"sellingTotal,omitempty"`
		// Total - total = base + totalTaxes.
		Total string `json:"total,omitempty"`
	}

	// Markup represents a markup applied by a stakeholder (travel agent, merchant mode, etc.).
	MarkupResponse struct {
		// Amount - the monetary value of the markup as a string with decimal.
		Amount string `json:"amount,omitempty"`
	}
)

// Common value types shared across the Amadeus hotel APIs. These are identical
// across the search, booking, and content schemas, so they live here and the
// per-module DTOs alias them instead of redefining them.

type (
	// MaxPersonCapacityResponse describes the occupancy capacity of a room.
	MaxPersonCapacityResponse struct {
		// Adults - maximum number of adults the room can accommodate.
		Adults int `json:"adults,omitempty"`
		// Children - maximum number of children the room can accommodate.
		Children int `json:"children,omitempty"`
		// Total - maximum total number of persons the room can accommodate.
		Total int `json:"total,omitempty"`
	}

	// MaxSleepFurnishingsResponse describes the extra sleeping furniture available.
	MaxSleepFurnishingsResponse struct {
		// Cribs - number of cribs available in the room.
		Cribs int `json:"cribs,omitempty"`
		// ExtraBeds - number of extra beds available in the room.
		ExtraBeds int `json:"extraBeds,omitempty"`
	}

	// RateFamilyEstimatedResponse describes the estimated rate family of an offer.
	RateFamilyEstimatedResponse struct {
		// Code - estimated rate family code. Examples: PRO, FAM, GOV. Pattern: [A-Z0-9]{3}.
		Code string `json:"code,omitempty"`
		// Type - type of the rate. P=public, N=negotiated, C=conditional. Pattern: [PNC].
		Type string `json:"type,omitempty"`
	}

	// WarningSourceResponse identifies the request element that triggered a warning.
	WarningSourceResponse struct {
		// Parameter - the key of the URI path or query parameter that caused the warning.
		Parameter string `json:"parameter,omitempty"`
		// Pointer - a JSON Pointer [RFC6901] to the associated entity in the request body.
		Pointer string `json:"pointer,omitempty"`
		// Example - a sample input to guide the user when resolving the issue.
		Example string `json:"example,omitempty"`
	}
)

type PricingTimeWindow string

const (
	PricingTimeWindowHourly     PricingTimeWindow = "HOURLY"
	PricingTimeWindowDaily      PricingTimeWindow = "DAILY"
	PricingTimeWindowMonthly    PricingTimeWindow = "MONTHLY"
	PricingTimeWindowWeekend    PricingTimeWindow = "WEEKEND"
	PricingTimeWindowWeekly     PricingTimeWindow = "WEEKLY"
	PricingTimeWindowFullPeriod PricingTimeWindow = "FULL_PERIOD"
)

type PricingMethod string

const (
	Daily                 PricingMethod = "DAILY"
	Hourly                PricingMethod = "HOURLY"
	HalfDay               PricingMethod = "HALF_DAY"
	AdditionsPerStay      PricingMethod = "ADDITIONS_PER_STAY"
	PerOccurrence         PricingMethod = "PER_OCCURRENCE"
	PerEvent              PricingMethod = "PER_EVENT"
	PerPerson             PricingMethod = "PER_PERSON"
	FirstUse              PricingMethod = "FIRST_USE"
	OneTimeUse            PricingMethod = "ONE_TIME_USE"
	PerMinute             PricingMethod = "PER_MINUTE"
	PerFunction           PricingMethod = "PER_FUNCTION"
	PerStay               PricingMethod = "PER_STAY"
	Complimentary         PricingMethod = "COMPLIMENTARY"
	Other                 PricingMethod = "OTHER"
	MaximumCharge         PricingMethod = "MAXIMUM_CHARGE"
	OverMinuteCharge      PricingMethod = "OVER_MINUTE_CHARGE"
	Weekly                PricingMethod = "WEEKLY"
	PerRoomPerStay        PricingMethod = "PER_ROOM_PER_STAY"
	PerRoomPerNight       PricingMethod = "PER_ROOM_PER_NIGHT"
	PerPersonPerStay      PricingMethod = "PER_PERSON_PER_STAY"
	PerPersonPerNight     PricingMethod = "PER_PERSON_PER_NIGHT"
	MinimumCharge         PricingMethod = "MINIMUM_CHARGE"
	PerRental             PricingMethod = "PER_RENTAL"
	PerItem               PricingMethod = "PER_ITEM"
	PerRoom               PricingMethod = "PER_ROOM"
	PerReservationBooking PricingMethod = "PER_RESERVATION_BOOKING"
	PerGallon             PricingMethod = "PER_GALLON"
	PerDozen              PricingMethod = "PER_DOZEN"
	PerTray               PricingMethod = "PER_TRAY"
	PerOrder              PricingMethod = "PER_ORDER"
	PerUnit               PricingMethod = "PER_UNIT"
	OneWay                PricingMethod = "ONE_WAY"
	RoundTrip             PricingMethod = "ROUND_TRIP"
)

type (
	ContentPriceResponse struct {
		// Base - base price of the amenity.
		Base string `json:"base,omitempty"`
		// Currency - currency code applied to the price.
		Currency CurrencyResponse `json:"currency,omitempty"`
		// Markups - markups applied to the amenity price.
		Markups []MarkupResponse `json:"markups,omitempty"`
		// SellingTotal - selling total = total + margins + markup + totalFees - discounts.
		SellingTotal string `json:"sellingTotal,omitempty"`
		// Total - total = base + totalTaxes.
		Total string `json:"total,omitempty"`
	}

	CurrencyResponse struct {
		Code string `json:"code"` // ISO currency code (http://www.iso.org/iso/home/standards/currency_codes.htm). For miles the code associated is MIL.
		Name string `json:"name"` // Indicates the name of the currency.
	}
)

type (
	DescriptionResponse struct {
		Text string `json:"text"`
		Lang string `json:"lang"`
	}

	// TextContentResponse models multilingual text content with metadata.
	TextContentResponse struct {
		Text string `json:"text,omitempty"`
		Lang string `json:"lang,omitempty"`
		// Character set. Enum: ASCII_7, UTF_8.
		CharSet string `json:"charSet,omitempty"`
		// Encoding. Enum: BINARY, BASE_64.
		Encoding        string `json:"encoding,omitempty"`
		IanaContentType string `json:"ianaContentType,omitempty"`
		Status          string `json:"status,omitempty"`
	}
)

// GeoCodeResponse is Amadeus's latitude/longitude pair, as sent by the content
// and inventory schemas.
type GeoCodeResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// FlexInt is an integer Amadeus may send either as a JSON number or as a quoted
// number.
//
// It is not consistent: roomQuantity arrives as 1 on some responses and as "2"
// on others, and the Enterprise guide shows both spellings. A plain int field
// rejects the quoted form, and since a response decodes as a whole, one quoted
// value discards everything else in it.
//
// A value that is neither form decodes as zero rather than failing, on the same
// reasoning: one odd field should not cost the caller the entire response.
type FlexInt int

// UnmarshalJSON accepts a number, a quoted number, or null.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	*f = 0
	if len(data) == 0 || string(data) == "null" {
		return nil
	}

	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*f = FlexInt(n)
	}
	return nil
}

// Int returns the value as a plain int, for mappers building domain types.
func (f FlexInt) Int() int { return int(f) }
