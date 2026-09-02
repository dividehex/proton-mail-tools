package bridge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"proton-mail-tools/internal/mail"
)

// IMAPStore implements mail.Store with one short-lived IMAP session per call,
// which keeps it stateless and safe for concurrent requests.
type IMAPStore struct {
	addr      string
	username  string
	password  string
	tlsConfig *tls.Config
}

// NewIMAPStore configures a store for the bridge IMAP endpoint.
func NewIMAPStore(addr, username, password string, tlsConfig *tls.Config) *IMAPStore {
	return &IMAPStore{addr: addr, username: username, password: password, tlsConfig: tlsConfig}
}

func (s *IMAPStore) connect() (*imapclient.Client, error) {
	c, err := imapclient.DialStartTLS(s.addr, &imapclient.Options{
		TLSConfig: s.tlsConfig,
		Dialer:    &net.Dialer{Timeout: dialTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to bridge IMAP %s: %w", s.addr, err)
	}
	if err := c.Login(s.username, s.password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("bridge IMAP login: %w", err)
	}
	return c, nil
}

func (s *IMAPStore) withClient(fn func(*imapclient.Client) error) error {
	c, err := s.connect()
	if err != nil {
		return err
	}
	defer c.Close()
	err = fn(c)
	_ = c.Logout().Wait()
	return err
}

func (s *IMAPStore) withMailbox(mailbox string, readOnly bool, fn func(*imapclient.Client) error) error {
	return s.withClient(func(c *imapclient.Client) error {
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: readOnly}).Wait(); err != nil {
			return fmt.Errorf("%w: %q (%v)", mail.ErrMailboxNotFound, mailbox, err)
		}
		return fn(c)
	})
}

// ListMailboxes lists selectable mailboxes with message counts where available.
func (s *IMAPStore) ListMailboxes(context.Context) ([]mail.Mailbox, error) {
	var out []mail.Mailbox
	err := s.withClient(func(c *imapclient.Client) error {
		caps := c.Caps()
		listStatus := caps.Has(imap.CapListStatus)
		opts := &imap.ListOptions{ReturnSpecialUse: caps.Has(imap.CapSpecialUse)}
		if listStatus {
			opts.ReturnStatus = &imap.StatusOptions{NumMessages: true, NumUnseen: true}
		}
		items, err := c.List("", "*", opts).Collect()
		if err != nil {
			return fmt.Errorf("list mailboxes: %w", err)
		}
		for _, item := range items {
			if hasAttr(item.Attrs, imap.MailboxAttrNoSelect) {
				continue
			}
			mb := mail.Mailbox{Name: item.Mailbox, Role: roleOf(item.Mailbox, item.Attrs)}
			status := item.Status
			if status == nil && !listStatus {
				if status, err = c.Status(item.Mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait(); err != nil {
					return fmt.Errorf("status %q: %w", item.Mailbox, err)
				}
			}
			if status != nil {
				mb.Total, mb.Unread = status.NumMessages, status.NumUnseen
			}
			out = append(out, mb)
		}
		return nil
	})
	return out, err
}

// Search returns the newest matching messages, most recent first.
func (s *IMAPStore) Search(_ context.Context, q mail.Query) ([]mail.Summary, error) {
	out := []mail.Summary{}
	err := s.withMailbox(q.Mailbox, true, func(c *imapclient.Client) error {
		data, err := c.UIDSearch(searchCriteria(q), nil).Wait()
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		uids, err := newestUIDs(c, data.AllUIDs(), q.Limit)
		if err != nil {
			return err
		}
		if len(uids) == 0 {
			return nil
		}
		msgs, err := c.Fetch(imap.UIDSetNum(uids...), summaryFetchOptions()).Collect()
		if err != nil {
			return fmt.Errorf("fetch summaries: %w", err)
		}
		for _, m := range msgs {
			out = append(out, summaryOf(q.Mailbox, m))
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
		return nil
	})
	return out, err
}

// Get fetches a full message without marking it read.
func (s *IMAPStore) Get(_ context.Context, mailbox string, uid uint32) (*mail.Message, error) {
	var msg *mail.Message
	err := s.withMailbox(mailbox, true, func(c *imapclient.Client) error {
		opts := summaryFetchOptions()
		opts.BodySection = []*imap.FetchItemBodySection{{Peek: true}}
		msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(uid)), opts).Collect()
		if err != nil {
			return fmt.Errorf("fetch message: %w", err)
		}
		if len(msgs) == 0 || len(msgs[0].BodySection) == 0 {
			return fmt.Errorf("%w: uid %d in %q", mail.ErrNotFound, uid, mailbox)
		}
		m := msgs[0]
		msg = &mail.Message{Summary: summaryOf(mailbox, m)}
		if env := m.Envelope; env != nil {
			msg.Cc = addresses(env.Cc)
			msg.ReplyTo = addresses(env.ReplyTo)
			if len(env.InReplyTo) > 0 {
				msg.InReplyTo = trimMsgID(env.InReplyTo[0])
			}
		}
		return parseBody(m.BodySection[0].Bytes, msg)
	})
	return msg, err
}

// UpdateFlags adds or removes system flags on the given messages.
func (s *IMAPStore) UpdateFlags(_ context.Context, mailbox string, uids []uint32, update mail.FlagUpdate) error {
	return s.withMailbox(mailbox, false, func(c *imapclient.Client) error {
		set := uidSet(uids)
		for _, step := range []struct {
			flag imap.Flag
			want *bool
		}{{imap.FlagSeen, update.Read}, {imap.FlagFlagged, update.Flagged}, {imap.FlagAnswered, update.Answered}} {
			if step.want == nil {
				continue
			}
			op := imap.StoreFlagsDel
			if *step.want {
				op = imap.StoreFlagsAdd
			}
			store := &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{step.flag}}
			if err := c.Store(set, store, nil).Close(); err != nil {
				return fmt.Errorf("store %s: %w", step.flag, err)
			}
		}
		return nil
	})
}

// Move relocates messages to destination, which must already exist.
func (s *IMAPStore) Move(_ context.Context, mailbox string, uids []uint32, destination string) error {
	return s.withMailbox(mailbox, false, func(c *imapclient.Client) error {
		if !c.Caps().Has(imap.CapMove) {
			return errors.New("bridge IMAP server does not advertise MOVE")
		}
		dest, err := c.List("", destination, nil).Collect()
		if err != nil {
			return fmt.Errorf("list %q: %w", destination, err)
		}
		if len(dest) == 0 {
			return fmt.Errorf("%w: destination %q", mail.ErrMailboxNotFound, destination)
		}
		if _, err := c.Move(uidSet(uids), destination).Wait(); err != nil {
			return fmt.Errorf("move to %q: %w", destination, err)
		}
		return nil
	})
}

// newestUIDs narrows uids to the limit most recent by INTERNALDATE. UID order
// cannot be trusted for recency: Bridge assigns UIDs in sync order, newest first.
func newestUIDs(c *imapclient.Client, uids []imap.UID, limit int) ([]imap.UID, error) {
	if len(uids) <= limit {
		return uids, nil
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, InternalDate: true}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch dates: %w", err)
	}
	dated := make([]datedUID, 0, len(msgs))
	for _, m := range msgs {
		dated = append(dated, datedUID{uid: m.UID, date: m.InternalDate})
	}
	return pickNewest(dated, limit), nil
}

type datedUID struct {
	uid  imap.UID
	date time.Time
}

func pickNewest(dated []datedUID, limit int) []imap.UID {
	sort.SliceStable(dated, func(i, j int) bool { return dated[i].date.After(dated[j].date) })
	if len(dated) > limit {
		dated = dated[:limit]
	}
	out := make([]imap.UID, 0, len(dated))
	for _, d := range dated {
		out = append(out, d.uid)
	}
	return out
}

func summaryFetchOptions() *imap.FetchOptions {
	return &imap.FetchOptions{
		UID:           true,
		Envelope:      true,
		Flags:         true,
		InternalDate:  true,
		RFC822Size:    true,
		BodyStructure: &imap.FetchItemBodyStructure{},
	}
}

func summaryOf(mailbox string, m *imapclient.FetchMessageBuffer) mail.Summary {
	sum := mail.Summary{
		Mailbox:  mailbox,
		UID:      uint32(m.UID),
		Date:     m.InternalDate,
		Size:     m.RFC822Size,
		Read:     hasFlag(m.Flags, imap.FlagSeen),
		Flagged:  hasFlag(m.Flags, imap.FlagFlagged),
		Answered: hasFlag(m.Flags, imap.FlagAnswered),
		From:     []mail.Address{},
		To:       []mail.Address{},
	}
	if env := m.Envelope; env != nil {
		if !env.Date.IsZero() {
			sum.Date = env.Date
		}
		sum.Subject = env.Subject
		sum.MessageID = trimMsgID(env.MessageID)
		sum.From = addresses(env.From)
		sum.To = addresses(env.To)
	}
	if m.BodyStructure != nil {
		sum.HasAttachments = hasAttachments(m.BodyStructure)
	}
	return sum
}

func searchCriteria(q mail.Query) *imap.SearchCriteria {
	crit := &imap.SearchCriteria{Since: q.Since, Before: q.Before}
	if len(q.UIDs) > 0 {
		crit.UID = []imap.UIDSet{uidSet(q.UIDs)}
	}
	if q.MessageID != "" {
		crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "Message-ID", Value: "<" + q.MessageID + ">"})
	}
	for _, h := range []struct{ key, value string }{{"From", q.From}, {"To", q.To}, {"Subject", q.Subject}} {
		if h.value != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: h.key, Value: h.value})
		}
	}
	if q.Text != "" {
		crit.Text = []string{q.Text}
	}
	if q.UnreadOnly {
		crit.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	return crit
}

func hasAttachments(bs imap.BodyStructure) bool {
	found := false
	bs.Walk(func(_ []int, part imap.BodyStructure) bool {
		if d := part.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			found = true
		}
		return !found
	})
	return found
}

var specialUseRoles = map[imap.MailboxAttr]string{
	imap.MailboxAttrAll:     mail.RoleAll,
	imap.MailboxAttrArchive: mail.RoleArchive,
	imap.MailboxAttrDrafts:  mail.RoleDrafts,
	imap.MailboxAttrFlagged: mail.RoleStarred,
	imap.MailboxAttrJunk:    mail.RoleSpam,
	imap.MailboxAttrSent:    mail.RoleSent,
	imap.MailboxAttrTrash:   mail.RoleTrash,
}

// Proton's fixed system folder names, used when SPECIAL-USE is not advertised.
var nameRoles = map[string]string{
	"inbox":    mail.RoleInbox,
	"sent":     mail.RoleSent,
	"drafts":   mail.RoleDrafts,
	"trash":    mail.RoleTrash,
	"archive":  mail.RoleArchive,
	"spam":     mail.RoleSpam,
	"starred":  mail.RoleStarred,
	"all mail": mail.RoleAll,
}

func roleOf(name string, attrs []imap.MailboxAttr) string {
	for _, attr := range attrs {
		if role, ok := specialUseRoles[attr]; ok {
			return role
		}
	}
	return nameRoles[strings.ToLower(name)]
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == want {
			return true
		}
	}
	return false
}

func hasFlag(flags []imap.Flag, want imap.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
	return set
}

func addresses(in []imap.Address) []mail.Address {
	out := make([]mail.Address, 0, len(in))
	for _, a := range in {
		if a.Host == "" { // group delimiters carry no deliverable address
			continue
		}
		out = append(out, mail.Address{Name: a.Name, Email: a.Addr()})
	}
	return out
}

func trimMsgID(id string) string {
	return strings.Trim(strings.TrimSpace(id), "<>")
}
