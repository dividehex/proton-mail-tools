package mail

import (
	"fmt"
	netmail "net/mail"
	"strings"
)

// ParseAddress accepts "user@example.com" or "Name <user@example.com>".
func ParseAddress(s string) (Address, error) {
	parsed, err := netmail.ParseAddress(strings.TrimSpace(s))
	if err != nil {
		return Address{}, fmt.Errorf("%w: address %q: %v", ErrInvalidInput, s, err)
	}
	return Address{Name: parsed.Name, Email: parsed.Address}, nil
}

// ParseAddresses parses each entry, failing on the first invalid one.
func ParseAddresses(list []string) ([]Address, error) {
	out := make([]Address, 0, len(list))
	for _, s := range list {
		addr, err := ParseAddress(s)
		if err != nil {
			return nil, err
		}
		out = append(out, addr)
	}
	return out, nil
}

// String renders the address in RFC 5322 form.
func (a Address) String() string {
	return (&netmail.Address{Name: a.Name, Address: a.Email}).String()
}

// Equal compares email addresses case-insensitively, ignoring display names.
func (a Address) Equal(other Address) bool {
	return strings.EqualFold(a.Email, other.Email)
}
