package offramp

import "time"

// UserListFilter drives paginated, filtered settlement listing for a single user.
type UserListFilter struct {
	Page                int
	PerPage             int
	SortField           string
	SortOrder           string
	Cursor              string
	Status              string
	DateFrom            *time.Time
	DateTo              *time.Time
	MinAmount           *string
	DestinationProvider string
	FiatCurrency        string
}
