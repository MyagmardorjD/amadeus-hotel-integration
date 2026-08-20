package codes

// Catalog exposes the code lists through the SDK value, so callers reach them
// the same way they reach the API services:
//
//	client, _ := sdk.New(sdk.Config{...})
//	for _, a := range client.Codes.Amenities() {
//	    fmt.Println(a, a.Label())
//	}
//
// The package-level All* functions are equivalent and need no SDK value or
// credentials. Use those when you only want the lists; use Catalog when it is
// convenient to pass the SDK around as one dependency.
//
// Every method returns static data compiled into the SDK. Nothing here calls
// Amadeus, so no method can fail or block.
//
// Distance units are absent by design: they are geo.AllUnits, and the subset a
// given endpoint accepts is enforced by that endpoint's own context.
type Catalog interface {
	Amenities() []Amenity
	Ratings() []Rating
	HotelSources() []HotelSource
	HotelSubTypes() []HotelSubType
	BoardTypes() []BoardType
	PaymentPolicies() []PaymentPolicy
	ContentViews() []ContentView
	// RateCodes returns the documented subset only; see AllRateCodes.
	RateCodes() []RateCode
}

type catalog struct{}

// NewCatalog returns the code catalog. It is stateless and safe for concurrent
// use.
func NewCatalog() Catalog { return catalog{} }

func (catalog) Amenities() []Amenity             { return AllAmenities() }
func (catalog) Ratings() []Rating                { return AllRatings() }
func (catalog) HotelSources() []HotelSource      { return AllHotelSources() }
func (catalog) HotelSubTypes() []HotelSubType    { return AllHotelSubTypes() }
func (catalog) BoardTypes() []BoardType          { return AllBoardTypes() }
func (catalog) PaymentPolicies() []PaymentPolicy { return AllPaymentPolicies() }
func (catalog) ContentViews() []ContentView      { return AllContentViews() }
func (catalog) RateCodes() []RateCode            { return AllRateCodes() }
