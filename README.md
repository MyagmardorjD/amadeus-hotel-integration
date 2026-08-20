# iTrip Hotel SDK for Go

A Go client for the [Amadeus Hotel APIs](https://developers.amadeus.com/):
finding hotels, pricing stays, describing properties and making reservations.

```go
client, err := sdk.New(sdk.Config{
    ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
    ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
    Environment:  sdk.Test,
})
if err != nil {
    return err
}

hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
```

Amadeus's JSON never reaches your code. Prices arrive as `money.Money`, dates as
`datetime.Date`, and enumerations as typed codes that fail to compile when wrong.

---

## Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Inventory — finding hotels](#inventory--finding-hotels)
- [Offers — pricing a stay](#offers--pricing-a-stay)
- [Content — describing a property](#content--describing-a-property)
- [Booking — making a reservation](#booking--making-a-reservation)
- [Value objects](#value-objects)
- [Search filter codes](#search-filter-codes)
- [Errors](#errors)
- [Configuration](#configuration)
- [Testing](#testing)
- [Migrating from the previous version](#migrating-from-the-previous-version)
- [Project structure](#project-structure)

---

## Requirements

- **Go 1.25** or later.
- **Amadeus Enterprise credentials.** These endpoints target the Enterprise
  travel gateway. Self-service credentials from the developer portal are
  rejected with a `403`.

No third-party dependencies. The SDK uses only the standard library.

## Installation

```bash
go get github.com/techpartners-asia/amadeus-hotel-integration/v2
```

---

## Architecture

The SDK is four **bounded contexts**, each a package you import for its own
types. They are drawn on the seams the business has, not on Amadeus's URLs.

| Context | Package | Answers | Amadeus API |
|---|---|---|---|
| **Inventory** | `inventory` | Which hotels exist, and where? | Hotel List v1.2, Hotel Name Autocomplete v1 |
| **Content** | `content` | What is this property like? | Hotel Content v3.1 |
| **Offers** | `offers` | What does a stay cost? | Hotel Search v3.5 |
| **Booking** | `booking` | Reserve it, and manage the reservation | Hotel Booking v2.x |

These four are the whole of the Amadeus Hotels Quick-Connect surface. In
particular **there is no guest-review or rating API**: the only "rating" Amadeus
exposes is the property's **star** rating, as a filter on Hotel List and a field
on content. Review text and scores come from a separate provider. (Amadeus does
publish a Hotel Ratings sentiment API, but it lives on the *self-service*
gateway, not the Enterprise one this SDK targets.)

```
        codes/  money/  geo/  datetime/  media/     <- shared value objects
              ^     ^     ^       ^        ^
   +----------+-----+-----+-------+--------+--------+
   |                                                |
inventory --> content                  offers --> booking
 (find it)     (describe it)          (price it)  (buy it)
```

Contexts never import each other's services — only values that cross the
boundary, such as a hotel ID or an offer ID.

### The anti-corruption layer

Every Amadeus wire structure lives under `internal/`. Go forbids importing
`internal/` from outside the module, so **you cannot accidentally depend on
Amadeus's JSON shapes** — it is a compile error, not a convention. Each
context's `mapper.go` is the only code that sees both sides.

What that buys you:

| Amadeus sends | You get |
|---|---|
| `"total": "120.50"` beside `"currency": "EUR"` | `money.Money` — exact, currency attached |
| `"checkInDate": "2026-08-10"` | `datetime.Date` — no timezone to shift it |
| `"isLoyaltyRate": "true"` (a **string**) | `bool` |
| `"boardType": "BREAKFAST"` + a doc comment listing the rest | `codes.BoardType` |
| `latitude` and `longitude` as separate floats | `geo.Coordinates`, or `nil` if unknown |
| `cancellation` **and** `cancellations`, often duplicated | one deduplicated list |

---

## Quick start

The usual flow — find hotels, price them, book one:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
    "github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
    "github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
    "github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
    "github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

func main() {
    client, err := sdk.New(sdk.Config{
        ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
        ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
        Environment:  sdk.Test,
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    // 1. Which hotels are in Paris?
    hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{
        CityCode: "PAR",
        Filters: inventory.Filters{
            Radius:  5,
            Ratings: []codes.Rating{codes.Rating4, codes.Rating5},
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // 2. What do they cost for three nights in August?
    checkIn := datetime.MustParseDate("2026-08-10")
    results, err := client.Offers.Search(ctx, offers.SearchQuery{
        HotelIDs: inventory.IDs(hotels)[:20], // at most 50 per search
        Stay:     offers.Stay{CheckIn: checkIn, CheckOut: checkIn.AddDays(3)},
        Guests:   offers.Guests{Adults: 2},
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, result := range results {
        offer, ok := result.Cheapest()
        if !ok {
            continue
        }
        refundable, certain := offer.Policies.IsRefundable()
        fmt.Printf("%-40s %10s  refundable=%v (certain=%v)\n",
            result.Hotel.Name, offer.Price.Total, refundable, certain)
    }
}
```

---

## Inventory — finding hotels

Four ways to find properties. The first three return `[]inventory.Hotel`;
`ByKeyword` returns `[]inventory.Suggestion` and is covered
[below](#autocomplete--matching-a-name-as-it-is-typed).

```go
// Around a city or airport code
hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{
    CityCode: "PAR",
    Filters: inventory.Filters{
        Radius:     10,
        RadiusUnit: geo.Kilometers,          // KM or MILE only
        ChainCodes: []string{"HL", "MC"},
        Amenities:  []codes.Amenity{codes.AmenitySwimmingPool},
        Ratings:    []codes.Rating{codes.Rating5},  // at most 4
        Source:     codes.HotelSourceDirectChain,
    },
})

// Around a point
hotels, err := client.Inventory.ByGeocode(ctx, inventory.GeocodeQuery{
    Position: geo.Coordinates{Latitude: 48.8566, Longitude: 2.3522},
    Filters:  inventory.Filters{Radius: 5},
})

// By property code
hotels, err := client.Inventory.ByIDs(ctx, inventory.IDsQuery{
    HotelIDs: []string{"MCLONGHM", "ACPAR419"},
})

// By name, as a user types it
suggestions, err := client.Inventory.ByKeyword(ctx, inventory.KeywordQuery{
    Keyword:     "PARI",
    CountryCode: "FR",
})
```

### What you get

```go
type Hotel struct {
    ID                 HotelID           // 8-char Amadeus property code
    Name               string
    ChainCode          string            // and BrandCode, MasterChainCode
    DupeID             string            // groups the same property across sources
    IATACode           string
    Rating             int               // star rating; 0 unless you filtered on Ratings
    Amenities          []codes.Amenity   // echoed back only if you filtered on Amenities
    Position           *geo.Coordinates  // nil when Amadeus cannot locate it
    Address            *Address          // nil when Amadeus sent none
    DistanceFromSearch *geo.Distance     // set by ByCity/ByGeocode, nil for ByIDs
    Sponsored          bool              // paid placement — worth surfacing
    LastUpdate         string
}
```

`Position` is a pointer on purpose. `0,0` is a real point in the Gulf of Guinea;
defaulting an unlocatable property to it would put it in the Atlantic and it
would pass a non-zero check.

`Rating` and `Amenities` are **echoes of your filter, not property facts**.
Amadeus returns the star rating on every hotel when the search filtered on
`Ratings`, and omits it entirely otherwise — so an unfiltered search leaves
`Rating` zero even for properties that do have stars. Same for `Amenities`. An
empty value is not evidence a property lacks them; use the content context for
the full picture.

`inventory.IDs(hotels)` extracts the property codes for the other contexts.

### Autocomplete — matching a name as it is typed

`ByKeyword` wraps the Hotel Name Autocomplete API: up to 20 hotels whose names
best match a partial keyword, for suggest-as-you-type on a search input.

```go
suggestions, err := client.Inventory.ByKeyword(ctx, inventory.KeywordQuery{
    Keyword:     "PARI",                    // required, 4–40 characters
    SubTypes:    []codes.HotelSubType{...}, // Leisure (aggregators), GDS (chains); empty = both
    CountryCode: "FR",                      // optional ISO 3166-1 alpha-2
    Language:    "FR",                      // optional ISO 639-1; falls back to English
    Max:         10,                        // 1–20; 0 = Amadeus's default of 20
})

for _, s := range suggestions {
    fmt.Println(s.Name, s.HotelIDs, s.Relevance)
}
```

A match is a `Suggestion`, not a `Hotel` — autocomplete matches names, so
there are no chain codes, ratings or distances:

```go
type Suggestion struct {
    Name      string
    HotelIDs  []HotelID           // usually one; leisure dupes list each code
    SubType   codes.HotelSubType  // which inventory it came from
    IATACode  string
    Relevance int                 // 1–100, higher = better match; results arrive best-first
    Position  *geo.Coordinates    // nil when Amadeus sent none
    Address   *Address            // city, state, country only; nil when absent
}
```

`suggestion.IDs()` hands the property codes to the offers or content context,
so pick-a-suggestion-then-price-it chains directly.

Two things worth knowing when wiring this to a UI:

- **Amadeus rejects keywords under 4 characters**, and the SDK rejects them
  locally with `ErrValidation` — so don't fire a request before the fourth
  keystroke, and debounce (~250 ms) so you don't spend a call per key.
- `subType` is mandatory on the wire; when `SubTypes` is empty the SDK sends
  both values, which is what autocomplete almost always wants.

> **Entitlement.** Hotel Name Autocomplete is a separate product on the
> Enterprise gateway, and a subscription that carries Hotel List does not
> automatically carry it. When it is missing, the gateway rejects the call with
> `ErrUnauthorized` — Amadeus error 38190, "Invalid access token" — even though
> the same token works for every other inventory call (measured 2026-08). Ask
> your Amadeus account manager to add it, or run the live suite's
> `TestInventoryByKeyword` to check a subscription.

---

## Offers — pricing a stay

```go
results, err := client.Offers.Search(ctx, offers.SearchQuery{
    HotelIDs: ids,                                    // required, <= 100
    Stay:     offers.Stay{CheckIn: in, CheckOut: out},
    Guests:   offers.Guests{Adults: 2, ChildAges: []int{7, 12}},
    Rooms:    1,

    BoardType:     codes.BoardTypeBreakfast,
    PaymentPolicy: codes.PaymentPolicyGuarantee,
    Currency:      "EUR",
    PriceRange:    "100-400",          // requires Currency

    BestRateOnly: codes.Ptr(false),    // false = every rate, not just cheapest
})
```

### Prices are money, not strings

```go
offer := results[0].Offers[0]

offer.Price.Payable()      // ← the price to charge the guest. Use this.
offer.Price.Total          // money.Money — "600 EUR", taxes included
offer.Price.Base           // room rate before non-included taxes; OFTEN ZERO
offer.Price.SellingTotal   // Total + agency markup; ZERO unless a markup applies

// Taxes carry their own semantics
taxes, _ := offer.Price.TaxesTotal()            // excludes taxes already in Base
onArrival, _ := offer.Price.PayableAtProperty() // city tax, tourist tax...

// A nightly rate, with the remainder rather than silent rounding
perNight, remainder, ok := offer.Price.PerNight(offer.Stay.Nights())
```

`PayableAtProperty` is frequently non-zero. Showing a guest only the booking
total understates what they will actually pay.

**How the amounts relate.** Amadeus documents only `total = base + totalTaxes`,
and never defines `base` itself. Measured across 277 live Enterprise offers, the
exact relationship is:

```
Total = Base + Σ(taxes where Included == false)
```

`Tax.Included` means the tax is **already inside `Base`**. That is what
`TaxesTotal()` sums — only the lines *not* already counted — so it equals
`Total - Base` precisely. It held on all 91 offers that carried both a base and
tax lines.

> **`Base` is absent on roughly a fifth of offers** (61 of 277 measured), and
> the two never appear together with itemised taxes on the same offer. So
> `Base + TaxesTotal()` does **not** reliably reconstruct `Total`, and a UI that
> prints `Base` will show `0` for many rates. Guard it:
>
> ```go
> if !offer.Price.Base.Amount().IsZero() {
>     // safe to show a base/tax split
> }
> ```

> **Which figure do I charge?** `offer.Price.Payable()`. It returns
> `SellingTotal` when a travel-agency markup applies and `Total` otherwise — and
> `Total` already includes taxes. **Do not display `SellingTotal` directly:**
> Amadeus leaves it zero unless a markup applies (it is zero on every
> self-service and GDS rate), so reaching for the customer-sounding field shows
> the guest a price of **0**. `Payable()` picks the right one; `HasMarkup()`
> tells you whether a markup was applied.

### What the search filters actually do

Measured against the live Enterprise sandbox, because several behave differently
from how they read.

| Filter | Behaviour |
|---|---|
| `BoardType` | Filters strictly. `ROOM_ONLY` returns only room-only offers |
| `PaymentPolicy` | Filters strictly. `GUARANTEE` returns only guarantee offers |
| `BestRateOnly` | Works. `true` returns one lead offer per hotel |
| `PriceRange` | **Per night, in the hotel's own currency — and not a guarantee.** See below |
| `RateCodes` | No observable effect in the sandbox; `RAC`, `PRO` and `COR` returned identical result sets |
| `Currency` | **Not a filter and does not convert prices.** See [Currency conversion](#currency-conversion) |

> **`PriceRange` does not bound the offers you get back.** It is applied per
> night, against the hotel's own currency rather than the one you requested, and
> it selects *hotels* rather than offers — a property whose lead rate qualifies
> returns all of its offers, including ones far outside the range. In one
> measured search, `priceRange=1-200` returned a hotel whose seventeen offers ran
> 474–535 per night, and the same hotel came back for `200-400` too.
>
> Treat it as a hint that narrows candidates, and filter client-side if you need
> real bounds:
>
> ```go
> nightly := offer.Price.Variations.Average.Total   // Amadeus's own per-night figure
> if nightly.Amount().IsZero() {
>     nightly, _, _ = offer.Price.Payable().Split(nights)
> }
> ```

Setting `PriceRange` without `Currency` is rejected by the SDK before any network
call, because Amadeus rejects it too.

### Refundability is answered honestly

```go
refundable, certain := offer.Policies.IsRefundable()
if !certain {
    // Amadeus did not say clearly. Do NOT tell the guest it is refundable.
}

if deadline, ok := offer.Policies.FreeCancellationUntil(); ok {
    fmt.Println("Free cancellation until", deadline)
}
```

Amadeus expresses refundability three different ways that do not always agree.
`IsRefundable` reconciles them and reports `certain=false` when it cannot —
because presenting a non-refundable rate as refundable costs a real person real
money.

### Grouping offers by room

An offer is a **bookable rate**, not a room. One room appears in many offers
differing by rate code, board type and cancellation policy. A room picker wants
the inverse:

```go
// Requires BestRateOnly: codes.Ptr(false) on the search.
for _, room := range results[0].GroupByRoom() {
    fmt.Printf("%s — from %s (%d rates)\n",
        room.Room.Description, room.PriceFrom, len(room.Offers))
}
```

Groups are ordered by cheapest price, then room type; offers within a group by
price, then ID. An offer with no usable price sorts last and never becomes the
"cheapest" unless it is the only one.

### Currency conversion

**Asking for a currency does not get you prices in it.** Setting
`SearchQuery.Currency` to `"MNT"` returns offers still priced in the hotel's own
currency, plus a rate in the response's `dictionaries` block:

```json
{"currency": "EUR", "total": "1410.61"}
"dictionaries": { "currencyConversionLookupRates": {
    "EUR": { "rate": "4099.1909999999998035", "target": "MNT", "targetDecimalPlaces": 0 }}}
```

**The SDK does not convert automatically.** `offer.Price` stays in the hotel's
own currency — it is the price the booking is charged and the figure that
reconciles against Amadeus, so it is never overwritten with a display estimate.
You convert when you want to show a figure:

```go
results, _ := client.Offers.Search(ctx, offers.SearchQuery{
    HotelIDs: ids,
    Currency: "MNT",
})

// For display — one call, correct currency, original preserved:
shown := results[0].DisplayTotal(offer)
fmt.Println(shown)              // "5782360 MNT"  (or the original if no rate)
if shown.Converted {
    // shown.Amount is MNT; shown.Original is the EUR the card is charged
}

// Or convert an explicit amount and handle the error yourself:
local, err := results[0].Rates.Convert(offer.Price.Total)
// 1410.61 EUR -> 5782360 MNT, rounded to whole tögrög
```

`Display` works on any money value from the offer — total, base, a tax, a
per-night rate — and returns the original unchanged (with `Converted: false`)
when no rate covers its currency, so you never render a bare number with the
wrong label.

Rounding follows `targetDecimalPlaces`: **0 for MNT and JPY**, which have no
minor unit, so a converted price lands on whole units rather than 5,782,359.82.

`Convert` **fails** rather than returning the unconverted amount when no rate
covers that currency — a search across several hotels can return more than one
currency while Amadeus supplies a rate for only some, and silently handing back
euros to a caller expecting tögrög shows a guest a price three orders of
magnitude out. Where a fallback is acceptable for display:

```go
amount, converted := results[0].Rates.ConvertOrOriginal(offer.Price.Total)
// converted=false means `amount` is still in the original currency — label it so
```

The rate Amadeus sends carries sixteen decimal places, most of it float noise.
It is held rounded to nine (`Rate`) with the original preserved (`RawRate`).

### Room capacity — how many people fit

Amadeus is unreliable here, so the SDK tells you where its answer came from
rather than pretending to certainty it does not have:

```go
capacity, source, ok := offer.MaxPersonCapacity()
switch {
case !ok:
    // Amadeus said nothing. Do not guess from Room.Beds.
case source == offers.CapacityStructured:
    // maxPersonCapacity was supplied. Trust it.
case source == offers.CapacityFromDescription:
    // Read out of prose like "Apartment with 1 bedroom for 4 persons".
    // A strong hint, not a guarantee.
}

fits, certain := offer.Accommodates(4)
```

**Why this is not a plain field.** Across a captured sandbox search of 44
offers:

| Source | Offers |
|---|---|
| `maxPersonCapacity` field | **0** |
| Stated in the description | 6 |
| No capacity at all | 38 |

The structured field exists in the schema and is mapped when present — it never
arrived once. Meanwhile the descriptions say things like *"Apartment with 1
bedroom for 4 persons"* in plain English.

Two traps this avoids:

- **`Room.Beds` is not capacity.** A room with two beds may sleep two, three or
  four. The captured apartment has `beds: 2` and sleeps 4.
- **`Guests` is not capacity.** It is the occupancy the price was *quoted for*,
  not what the room holds.

**When it matters commercially, ask Amadeus rather than the SDK.** Search with
the occupancy you actually want — its filtering is more dependable than
anything it publishes about capacity:

```go
results, err := client.Offers.Search(ctx, offers.SearchQuery{
    HotelIDs: ids,
    Guests:   offers.Guests{Adults: 4},   // authoritative: it prices or it doesn't
})
```

### Re-checking a price before booking

```go
detail, err := client.Offers.Get(ctx, offers.GetQuery{OfferID: offer.ID})
if errors.Is(err, sdk.ErrNotFound) {
    // The offer expired. Search again.
}
```

**Offer IDs expire.** Treat one as good for the current user session and no
longer.

---

## Content — describing a property

```go
hotel, err := client.Content.Get(ctx, content.Query{
    HotelID: "HLPAR266",
    Lang:    "FR",         // falls back to English where untranslated
})
```

The SDK requests the `FULL` view by default. Amadeus's own default returns only
the basic block, which is almost never what you want.

```go
hotel.Name, hotel.Rating, hotel.Description
hotel.Rooms            // room types, with dimensions, occupancy, amenities
hotel.Facilities       // meeting rooms, restaurants, shared amenities
hotel.Policies         // payment, check-in, pets, tax, guest age rules
hotel.Awards           // ratings and certifications
hotel.PointsOfInterest // notable places nearby
hotel.NearbyLandmarks  // with distances, comparable across units

if position, ok := hotel.Position(); ok { /* ... */ }
if photo, ok := hotel.PrimaryPhoto(); ok {
    thumbnail := photo.Best(400)  // picks a rendition, not the original
}
```

**Almost every field is optional.** What a property publishes varies enormously
by source: a chain hotel returns rooms, facilities and fifty photographs; an
aggregator listing returns a name and an address. Absent blocks are `nil`, so
you can tell "not published" from "published empty".

### Room photographs are usually not on the room

Amadeus mixes prose blocks into the same `media` array as photographs, so both
`Hotel.Media` and `Room.Media` are split: everything left in `Media` carries a
real image URL, and the text moves to `Descriptions`, tagged with Amadeus's own
label.

The catch is where the photographs live. Across five measured properties, **named
bookable rooms carried no images at all** — Amadeus attaches room photography to
the property, or to unnamed room entries, and gives the named rooms a
`DEFAULT_ROOM_NAME` text block instead. So an empty `room.Media` is normal:

```go
photos := room.Media          // images only; may legitimately be empty
if len(photos) == 0 {
    photos = hotel.Media      // the property's pool, where room shots usually are
}
```

`Hotel.Media` is not sorted by subject either — properties commonly label every
photograph `MISCELLANEOUS`, so guest-room shots cannot be separated from lobby
shots by category.

Content does not change per stay, so it is worth caching. An offer is not.

> Property-level `Policies` here are descriptive. The terms that actually bind a
> reservation are on the **offer**. Where the two disagree, the offer wins.

---

## Booking — making a reservation

> Against `sdk.Production` this charges a real card and creates a real
> reservation.

```go
order, err := client.Booking.Create(ctx, booking.Reservation{
    Guests: []booking.Guest{{
        ID:        1,                   // your own ID, referenced by Rooms
        Title:     "MS",
        FirstName: "Ada",
        LastName:  "Lovelace",
        Email:     "ada@example.com",
        Phone:     "+33679278416",
    }},
    Rooms: []booking.RoomRequest{{
        OfferID:        offer.ID.String(),
        GuestIDs:       []int{1},       // first is the main guest
        SpecialRequest: "High floor if possible",
    }},
    Payment: booking.Payment{
        Method: booking.PaymentCreditCard,
        Card: &booking.Card{
            VendorCode: "VI",
            Number:     "4111111111111111",
            Expiry:     "1230",         // MMYY or YYYY-MM
            HolderName: "ADA LOVELACE",
            ThreeDS:    &booking.ThreeDSecure{ /* mandatory for EU cards */ },
        },
    },
    Agent: booking.Agent{Email: "agency@example.com"},
})
```

**Persist `order.ID` immediately.** It is the only handle to the reservation;
without it you cannot retrieve or cancel.

### Checking a booking

```go
b := order.Bookings[0]

b.Status.IsActive()          // CONFIRMED, ON_HOLD or PAST
b.Status.IsCancelled()

if number, ok := b.ConfirmationNumber(); ok {
    // What the guest quotes at the desk
}
```

`IsActive()` deliberately **excludes `PENDING`**. An on-request booking the
hotel has not accepted is not a room, and reporting it as one sends a guest to a
property with no reservation.

Retrieve an order either way:

```go
order, err := client.Booking.Get(ctx, "XN_5FGHIJKLMN")        // by Amadeus order ID
order, err := client.Booking.GetByReference(ctx, "3HHCAJ")    // by GDS PNR locator
```

`GetByReference` sends the locator as a query parameter beside
`originSystemCode=GDS`, which is what the Enterprise gateway requires; it also
unwraps the array this endpoint returns and reports an unknown locator as
`ErrNotFound` rather than as a blank order, since Amadeus answers one with `200`
and an empty element.

### Managing it

```go
order, err := client.Booking.Get(ctx, orderID)
order, err := client.Booking.GetByReference(ctx, "JKL789")

// Will cancelling cost anything, right now?
free, certain := b.Offer.Policies.CanCancelFreeOfCharge(time.Now())

order, err := client.Booking.Cancel(ctx, orderID, bookingID)
order, err := client.Booking.Modify(ctx, orderID, bookingID, booking.Modification{
    Stay: &newStay,   // only the fields you set are sent
})
result, err := client.Booking.Delete(ctx, orderID, bookingID)
```

`CanCancelFreeOfCharge` takes the current time because the guest's real question
is "can I *still* cancel?", which depends on the deadline not having passed —
not merely on one existing.

### Validation before anything is charged

Booking validates harder than the rest of the SDK. A malformed search costs a
round trip; a malformed booking either fails after the guest was told it
succeeded, or succeeds on terms nobody chose. Checked locally:

- Guest IDs unique, and every room's guest reference resolves.
- Card numbers pass the **Luhn checksum** — a transposed digit becomes a named
  error rather than an opaque decline.
- Names match Amadeus's letters-and-spaces rule (which rejects accents).
- Expiry, CVV and vendor-code formats.

**Card numbers never appear in an error message.** Validation errors get logged;
a card number must not travel with them.

---

## Value objects

### `money` — exact prices

Amadeus sends prices as decimal strings. `strconv.ParseFloat` loses precision on
values a hotel bill can contain, so `money.Amount` is fixed-point.

```go
total := offer.Price.Total          // money.Money
total.Amount()                      // exact decimal
total.Currency()                    // "EUR"
total.String()                      // "600 EUR"

sum, err := a.Add(b)                   // errors on mixed currencies
part, remainder, ok := total.Split(3)  // remainder returned, not rounded away
```

`0.1 + 0.2` is exactly `0.3` here.

### `datetime` — calendar dates

A check-in date is a date, not an instant. Parsing `"2026-08-10"` into a
`time.Time` forces a timezone on it, and formatting it elsewhere can move the
booking by a day.

```go
checkIn := datetime.MustParseDate("2026-08-10")
nights := checkIn.DaysUntil(checkOut)
checkIn.AddDays(3)
```

### `geo` — coordinates and distances

```go
geo.Coordinates{Latitude: 48.8566, Longitude: 2.3522}
distance.Meters()   // compare a radius quoted in miles against one in km
```

### `media` — images and text

```go
photo.Best(400)   // nearest rendition <= 400px, rather than the original
photo.Alt         // the only accessible description Amadeus supplies
```

---

## Search filter codes

Typed constants for everything Amadeus accepts in a filter, so a wrong code
fails to compile instead of returning a `400`.

```go
codes.AmenitySwimmingPool
codes.Rating5
codes.BoardTypeBreakfast
codes.PaymentPolicyGuarantee
codes.HotelSourceDirectChain
codes.ContentViewFull
codes.RateCodePublic
```

Enumerate them for a filter UI, with or without a client:

```go
for _, a := range codes.AllAmenities() {
    fmt.Println(a, a.Label())   // "SWIMMING_POOL  Swimming Pool"
}

// Or through the client, if that is more convenient to pass around
client.Codes.Amenities()
```

These are static data compiled into the SDK. Nothing here calls Amadeus.

### Two codes differ from the Amadeus documentation

The live API **rejects** two codes exactly as Amadeus documents them:

| Documented | Actually accepted |
|---|---|
| `BABY-SITTING` | `BABY_SITTING` |
| `BAR or LOUNGE` | `BAR_LOUNGE` |

The constants use the working forms.

### Rate codes are not a closed set

`codes.RateCode` accepts any 3-character code, because corporate codes are
negotiated per account and cannot be enumerated. `AllRateCodes()` returns the
documented public and qualified rates only — a starting list for a UI, not a
whitelist. `IsValid()` checks the *shape*, not membership.

---

## Errors

Two ways to inspect a failure.

```go
// Which kind of failure was it?
if errors.Is(err, sdk.ErrNotFound)    { /* no such hotel, or an expired offer */ }
if errors.Is(err, sdk.ErrRateLimited) { /* back off */ }
if errors.Is(err, sdk.ErrServer)      { /* Amadeus's side — worth retrying */ }
if errors.Is(err, sdk.ErrValidation)  { /* your request; never sent */ }

// What exactly did Amadeus say?
var apiErr *sdk.APIError
if errors.As(err, &apiErr) {
    for _, d := range apiErr.Details {
        fmt.Println(d.Code, d.Title, d.Detail, d.Source)
    }
}
```

| Sentinel | HTTP | Meaning |
|---|---|---|
| `ErrValidation` | — | Rejected by the SDK before sending; names the field |
| `ErrInvalidRequest` | 400 | Amadeus rejected the request |
| `ErrUnauthorized` | 401 | Bad credentials, or a token that could not refresh |
| `ErrForbidden` | 403 | Valid credentials, not entitled. Self-service keys land here |
| `ErrNotFound` | 404 | No such hotel, offer or order — including an expired offer |
| `ErrRateLimited` | 429 | Per-second or per-month quota |
| `ErrServer` | 5xx | Amadeus's side |

Validation failures name the offending field, and report **every** problem at
once rather than one per round trip:

```go
var errs sdk.ValidationErrors
if errors.As(err, &errs) {
    for _, e := range errs {
        fmt.Printf("%s: %s\n", e.Field, e.Reason)
    }
}
```

Amadeus sometimes returns `200` with an `errors` array and no usable data. The
SDK treats that as a failure, not a success with an empty result.

---

## Configuration

```go
sdk.Config{
    ClientID:     "...",              // required
    ClientSecret: "...",              // required
    Environment:  sdk.Production,     // defaults to sdk.Test

    HTTPClient: myClient,             // proxying, TLS, instrumentation
    Timeout:    90 * time.Second,     // used when HTTPClient is nil; default 60s
    UserAgent:  "my-app/1.0",

    SkipCredentialCheck: false,       // true defers auth to the first call
}
```

`Environment` defaults to `Test` deliberately: a forgotten setting gives you
sandbox data, not a real charge.

Several clients can coexist in one process — different environments, different
tenants — each with its own token.

```go
if err := client.Ping(ctx); err != nil { /* health check */ }
```

Every method takes a `context.Context` as its first argument, so cancellation,
deadlines and tracing all work.

### Logging requests and responses

Pass an `slog.Logger` and every request and response is logged at `Debug`:

```go
client, _ := sdk.New(sdk.Config{
    ClientID:     id,
    ClientSecret: secret,
    Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })),
})
```

```json
{"level":"DEBUG","msg":"amadeus request","method":"GET","url":".../hotels/by-city?cityCode=PAR"}
{"level":"DEBUG","msg":"amadeus response","method":"GET","url":"...","status":200,"elapsed":"1.06s","bytes":14063}
```

It logs the method, URL, status, timing and size of each call. Leave your
handler at `Info` or higher and the per-call traffic log goes quiet — and
nothing is spent formatting it, because the work is skipped when `Debug` is
disabled.

**Failures are logged at `Error`, whatever level you set.** `Debug` is off in
production, which is exactly where a failed booking has to be explicable
afterwards, so a failed call logs both halves — the request that caused it and
the response that came back:

```json
{"level":"ERROR","msg":"amadeus call failed","method":"GET",
 "url":".../v3/shopping/hotel-offers?...&hotelIds=ZZZZZZZZ","status":400,
 "response":"{\"errors\":[{\"code\":3237,\"detail\":\"Property codes not found in system\"}]}",
 "request":"{\"data\":{...}}"}
```

The request body is redacted by the same rules as the `Debug` log — logging more
on failure must not become a way for a card number to reach a log. A rejected
authentication exchange logs `amadeus authentication failed` with its response
only, never its request, which carries the client secret.

**Response bodies are logged only where they earn their place**, because one
search runs to hundreds of kilobytes and a reservation carries guest names,
emails and phone numbers:

| Call | Body logged |
|---|---|
| Any failure — a non-2xx, or one of the 200s Amadeus sends carrying an `errors` array | **Yes.** It is the only account of what Amadeus objected to |
| Anything under `/v2/booking/` — create, retrieve, modify, cancel | **Yes.** A booking took money, so its response is the audit trail |
| A successful search, content or inventory call | No — method, status, timing and `bytes` only |

**Sensitive data is redacted, not logged.** This is the same guarantee the
booking validation makes about error messages, extended to the log:

- **Request bodies** have the card number, security code and 3DS cryptogram
  replaced with `[REDACTED]` before logging. A body that cannot be parsed is
  omitted, never emitted raw.
- **The authentication exchange logs no body at all** — its request carries the
  client secret and its response carries the access token, so it logs status
  only.
- **A logged booking response is emitted as received**, where Amadeus has
  already masked card numbers. It still contains guest contact details, so
  treat the log as personal data.

---

## Testing

```bash
go test ./...                    # offline; no credentials, no network
```

Three layers:

1. **Mapper tests** (default) — each context decodes a recorded Amadeus response
   from its `testdata/` and asserts field by field. This is the regression
   suite, and it runs anywhere.

2. **Fixture capture** (on demand) — record fresh responses from the sandbox:

   ```bash
   AMADEUS_CLIENT_ID=... AMADEUS_CLIENT_SECRET=... go run ./internal/capture
   go test ./...   # a failure now is a field Amadeus changed
   ```

   It never captures booking responses: creating an order on the sandbox is
   still a booking, and a tool must not make one as a side effect.

3. **Live suite** (build-tagged) — end-to-end against the sandbox:

   ```bash
   AMADEUS_CLIENT_ID=... AMADEUS_CLIENT_SECRET=... go test -tags live ./livetest/ -v
   ```

Testing your own code against the SDK: every service is an interface, so
substitute a fake.

```go
type stubOffers struct{ offers.Service }

func (stubOffers) Search(context.Context, offers.SearchQuery) ([]offers.HotelOffers, error) {
    return []offers.HotelOffers{ /* ... */ }, nil
}
```

---

## Migrating from the previous version

v2 is a breaking release: the public API changed completely. Everything below is
a straight substitution.

### The import path gains a `/v2`

Go requires this for any major version above v1, so it is the first change to
make:

```go
// before
import sdk "github.com/techpartners-asia/amadeus-hotel-integration"

// after
import sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
```

The repository is unchanged; only the module path carries the suffix. v1.2.0
keeps working untouched, so you can upgrade when it suits you:

```bash
go get github.com/techpartners-asia/amadeus-hotel-integration/v2@v2.0.0
```

### Construction

```go
// before
client, err := sdk.New(id, secret)

// after
client, err := sdk.New(sdk.Config{ClientID: id, ClientSecret: secret})
```

Production used to require editing `constants/url.go`. Now set
`Environment: sdk.Production`.

### Method names

| Before | After |
|---|---|
| `client.List.HotelListByCityCode(req)` | `client.Inventory.ByCity(ctx, query)` |
| `client.List.HotelListByGeocode(req)` | `client.Inventory.ByGeocode(ctx, query)` |
| `client.List.HotelListByHotelIds(req)` | `client.Inventory.ByIDs(ctx, query)` |
| `client.Offers.List(req)` | `client.Offers.Search(ctx, query)` |
| `client.Offers.GetByID(req)` | `client.Offers.Get(ctx, query)` |
| `client.Content.GetByID(req)` | `client.Content.Get(ctx, query)` |
| `client.Booking.Create(req)` | `client.Booking.Create(ctx, reservation)` |
| `client.SearchCriteria` | `client.Codes` |
| `searchcriteria.*` | `codes.*` |

**Every method now takes a `context.Context` first.**

### Types

| Before | After |
|---|---|
| `responseHotelListDTO.GeneralInfoResponse` | `inventory.Hotel` |
| `responseHotelOffersDTO.OffersResponse` | `offers.HotelOffers` |
| `responseContentDTO.HotelContentResponse` | `content.Hotel` |
| `responseBookingDTO.HotelOrder` | `booking.Order` |
| `sharedResponseDTO.APIError` | `sdk.APIError` |
| `searchcriteria.RadiusUnit` | `geo.Unit` |

### Field changes to expect

- **Prices** are `money.Money`, not `string`. `offer.Price.Total.String()` for
  display; `.Amount()` and `.Currency()` for the parts.
- **Dates** are `datetime.Date`, not `string`.
- **`IsLoyaltyRate`** is a `bool`, not the string `"true"`.
- **`GeoCode`** is `*geo.Coordinates` and is `nil` when unknown — check it.
- **Cancellation policies** are one merged `[]CancellationPolicy` rather than
  separate `Cancellation` and `Cancellations` fields.
- **`Offers.Get`** returns `*OfferDetail` (hotel plus offer), not a bare offer.
- **`modules/list`** is now `inventory`; **`Services`** on an offer is now
  `Extras`.

### Behaviour that changed

- Invalid requests are rejected locally with `ErrValidation` before any network
  call, instead of returning an Amadeus `400`.
- A `200` response carrying an `errors` array is now an error.
- A `401` on a cached token triggers one silent refresh and retry.

---

## Project structure

```
sdk.go        Client, error re-exports
config.go     Config, Environment

inventory/    which hotels exist, and where
content/      what a property is like
offers/       what a stay costs
booking/      reservations

money/        exact prices
geo/          coordinates and distances
datetime/     calendar dates
media/        images and text
codes/        search filter enumerations
apierr/       error types and sentinels

internal/
  amadeus/      transport, auth, error decoding
    dto/        Amadeus wire structures — unreachable by callers
  mapping/      shared wire-to-domain primitives
  amadeustest/  fixture-backed test server
  capture/      records live responses into testdata

livetest/     end-to-end suite (-tags live)
docs/         design records
```

Each context holds its domain types, its service, and a `mapper.go` — the only
file that sees both the wire format and the domain.

---

## License

MIT. See [LICENSE](LICENSE).
