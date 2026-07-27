package offers

import (
	"sort"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
)

// Grouping offers by room is domain logic, not presentation. Amadeus returns a
// flat list of offers per hotel, where an offer is a bookable rate rather than
// a room: one room (Room.Type, e.g. "C3S") appears in many offers differing by
// rate code, board type, cancellation policy and price. A room-picker shows the
// inverse - each room once, with its rate options underneath - and computing
// that inversion correctly, including which offer is genuinely cheapest, is the
// kind of decision that belongs in the domain rather than in each caller's UI
// layer.
//
// Grouping is only meaningful when the search set BestRateOnly to false. With
// Amadeus's default of true the API returns one cheapest offer per hotel, so
// every hotel collapses to a single group of a single offer.

// GroupedHotel is one hotel with its offers regrouped by room.
type GroupedHotel struct {
	// Hotel is the property the offers belong to.
	Hotel Hotel
	// Available reports whether the property has bookable inventory.
	Available bool
	// Rooms are the room groups, cheapest first.
	Rooms []RoomGroup
}

// RoomGroup collects every offer for one room and precomputes what a room card
// shows before it is expanded.
type RoomGroup struct {
	// RoomType is the Room.Type shared by every offer in the group. Offers
	// whose room type is empty are collected under "".
	RoomType string
	// Room is copied from the cheapest offer, so a caller can render the
	// description, bed type and category without reaching into Offers.
	Room Room
	// PriceFrom is the cheapest offer's total, and is the "from" price a room
	// card displays. It is zero only when no offer in the group had a price.
	PriceFrom money.Money
	// Cheapest is the lowest-priced offer, which is Offers[0]. It is nil only
	// for an empty group, which cannot occur through GroupByRoom.
	Cheapest *Offer
	// Offers are the room's rate options, cheapest first, with the offer ID as
	// a tie-breaker so the order is stable across identical responses.
	Offers []Offer
}

// GroupByRoom regroups one hotel's offers by room type.
//
// The result is deterministic: groups are ordered by their cheapest price then
// room type, and each group's offers by price then ID. An offer with no usable
// price sorts last and is never chosen as the cheapest unless it is the only
// offer in its group, so a missing price cannot mask a real one.
func (h HotelOffers) GroupByRoom() []RoomGroup {
	grouped := h.groupBy(func(r Room) string { return r.Type })
	out := make([]RoomGroup, len(grouped))
	for i, g := range grouped {
		out[i] = RoomGroup{
			RoomType:  g.key,
			Room:      g.room,
			PriceFrom: g.priceFrom,
			Cheapest:  g.cheapest,
			Offers:    g.offers,
		}
	}
	return out
}

// keyedGroup is the key-agnostic result of grouping and ordering one hotel's
// offers. GroupByRoom and GroupByCategory differ only in the key they choose,
// so both build their typed groups from this.
type keyedGroup struct {
	key       string
	room      Room
	priceFrom money.Money
	cheapest  *Offer
	offers    []Offer
}

// groupBy buckets the hotel's offers by keyOf(Room), orders each bucket cheapest
// first with the offer ID as a tie-breaker, and orders the buckets by their
// cheapest price then key. An offer with no usable price sorts last and is never
// chosen as the cheapest unless it is the only offer in its group. First-seen
// order is recorded so grouping stays deterministic even when every offer lacks
// a price and the sort has nothing to order on.
func (h HotelOffers) groupBy(keyOf func(Room) string) []keyedGroup {
	order := make([]string, 0, len(h.Offers))
	byKey := make(map[string][]Offer, len(h.Offers))

	for _, offer := range h.Offers {
		key := keyOf(offer.Room)
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], offer)
	}

	groups := make([]keyedGroup, 0, len(order))
	for _, key := range order {
		offers := byKey[key]
		sort.SliceStable(offers, func(i, j int) bool {
			if cmp := comparePrices(offers[i].Price.Total, offers[j].Price.Total); cmp != 0 {
				return cmp < 0
			}
			return offers[i].ID < offers[j].ID
		})

		group := keyedGroup{key: key, offers: offers}
		if len(offers) > 0 {
			group.cheapest = &group.offers[0]
			group.room = group.cheapest.Room
			group.priceFrom = group.cheapest.Price.Total
		}
		groups = append(groups, group)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if cmp := comparePrices(groups[i].priceFrom, groups[j].priceFrom); cmp != 0 {
			return cmp < 0
		}
		return groups[i].key < groups[j].key
	})

	return groups
}

// GroupByRoom regroups every hotel in a search result, preserving the order in
// which Amadeus returned the hotels.
func GroupByRoom(hotels []HotelOffers) []GroupedHotel {
	out := make([]GroupedHotel, 0, len(hotels))
	for _, hotel := range hotels {
		out = append(out, GroupedHotel{
			Hotel:     hotel.Hotel,
			Available: hotel.Available,
			Rooms:     hotel.GroupByRoom(),
		})
	}
	return out
}

// CategorizedHotel is one hotel with its offers regrouped by room category.
type CategorizedHotel struct {
	// Hotel is the property the offers belong to.
	Hotel Hotel
	// Available reports whether the property has bookable inventory.
	Available bool
	// Categories are the category groups, cheapest first.
	Categories []CategoryGroup
}

// CategoryGroup collects every offer for one room category. It mirrors
// RoomGroup but groups on the room's category rather than its exact type code,
// so a picker can show "Suite" or "Standard Room" once with every rate and
// every type variant beneath it.
type CategoryGroup struct {
	// Category is the key every offer in the group shares: the room's estimated
	// category ("SUITE", "STANDARD_ROOM"). A room that carries no category - a
	// wildcard code such as "*1B", or "ROH" - keeps its own code so it forms its
	// own group instead of collapsing with unrelated rooms; a room with neither
	// falls to "OTHER". See categoryKey.
	Category string
	// Room is copied from the cheapest offer, as in RoomGroup.
	Room Room
	// PriceFrom is the cheapest offer's total - the "from" price a category card
	// shows. It is zero only when no offer in the group had a price.
	PriceFrom money.Money
	// Cheapest is the lowest-priced offer, which is Offers[0]. It is nil only for
	// an empty group, which cannot occur through GroupByCategory.
	Cheapest *Offer
	// Offers are every rate for every room in the category, cheapest first with
	// the offer ID as a stable tie-breaker.
	Offers []Offer
}

// GroupByCategory regroups one hotel's offers by room category.
//
// It orders groups and offers exactly as GroupByRoom does; categoryKey decides
// which category an offer belongs to, including how uncategorised rooms fall
// back so they are never lost or merged with unrelated inventory.
func (h HotelOffers) GroupByCategory() []CategoryGroup {
	grouped := h.groupBy(categoryKey)
	out := make([]CategoryGroup, len(grouped))
	for i, g := range grouped {
		out[i] = CategoryGroup{
			Category:  g.key,
			Room:      g.room,
			PriceFrom: g.priceFrom,
			Cheapest:  g.cheapest,
			Offers:    g.offers,
		}
	}
	return out
}

// GroupByCategory regroups every hotel in a search result, preserving the order
// in which Amadeus returned the hotels.
func GroupByCategory(hotels []HotelOffers) []CategorizedHotel {
	out := make([]CategorizedHotel, 0, len(hotels))
	for _, hotel := range hotels {
		out = append(out, CategorizedHotel{
			Hotel:      hotel.Hotel,
			Available:  hotel.Available,
			Categories: hotel.GroupByCategory(),
		})
	}
	return out
}

// categoryKey chooses the group key for a room. It prefers the estimated
// category; failing that the room type code, so an uncategorised room forms its
// own group rather than merging with unrelated ones (a wildcard "*1B" and a
// "ROH" must not share a bucket); failing both, "OTHER".
func categoryKey(r Room) string {
	switch {
	case r.Category != "":
		return r.Category
	case r.Type != "":
		return r.Type
	default:
		return "OTHER"
	}
}

// comparePrices orders two prices, sorting a missing price last so it never
// displaces a genuine cheapest offer.
//
// Prices in different currencies cannot be ordered; those compare equal, which
// leaves the ID tie-breaker to produce a stable result rather than an arbitrary
// one. A single Amadeus response quotes one currency per hotel, so this is the
// degenerate case rather than the common one.
func comparePrices(a, b money.Money) int {
	aMissing, bMissing := a.Amount().IsZero(), b.Amount().IsZero()
	switch {
	case aMissing && bMissing:
		return 0
	case aMissing:
		return 1
	case bMissing:
		return -1
	}

	cmp, err := a.Compare(b)
	if err != nil {
		return 0
	}
	return cmp
}
