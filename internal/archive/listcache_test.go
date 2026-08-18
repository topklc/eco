package archive

import (
	"fmt"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestListCacheStreams pins the LIST path: results go to disk in chunks as
// they arrive and are read back a page at a time, so a network with 50,000
// channels never becomes 50,000 objects in RAM (§5.4).
func TestListCacheStreams(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	fetched := epoch
	for chunk := 0; chunk < 5; chunk++ {
		var batch []model.ChannelInfo
		for i := 0; i < 200; i++ {
			n := chunk*200 + i
			batch = append(batch, model.ChannelInfo{
				Name:  fmt.Sprintf("#chan%04d", n),
				Users: n,
				Topic: fmt.Sprintf("topic for %d", n),
			})
		}
		if err := db.PutChannels(ctx, batch, fetched); err != nil {
			t.Fatalf("chunk %d: %v", chunk, err)
		}
	}

	got, err := db.Channels(ctx, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("Channels(limit 5) returned %d rows", len(got))
	}
	if got[0].Name != "#chan0999" || got[0].Users != 999 {
		t.Errorf("the busiest channel is %+v, want #chan0999 with 999 users", got[0])
	}

	// A re-LIST refreshes rather than duplicates.
	if err := db.PutChannels(ctx, []model.ChannelInfo{
		{Name: "#chan0999", Users: 3, Topic: "quieter now"},
	}, fetched.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	all, err := db.Channels(ctx, "", maxPageLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1000 {
		t.Errorf("after a partial re-LIST the cache holds %d channels, want 1000", len(all))
	}
	for _, c := range all {
		if c.Name == "#chan0999" && (c.Users != 3 || c.Topic != "quieter now") {
			t.Errorf("the re-listed channel was not updated: %+v", c)
		}
	}

	when, err := db.ListFetchedAt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !when.Equal(fetched.Add(time.Hour)) {
		t.Errorf("ListFetchedAt = %s, want the newest fetch %s", when, fetched.Add(time.Hour))
	}
}

// TestListCacheSearch pins the Discover box: it matches names and topics, and
// a wildcard the user types is a character, not a pattern.
func TestListCacheSearch(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	if err := db.PutChannels(ctx, []model.ChannelInfo{
		{Name: "#eco", Users: 40, Topic: "the eco client"},
		{Name: "#gophers", Users: 300, Topic: "go, and eco talk sometimes"},
		{Name: "#100%", Users: 5, Topic: "literal percent"},
		{Name: "#quiet", Users: 1, Topic: ""},
		{Name: "", Users: 9, Topic: "nameless rows are skipped"},
	}, epoch); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		match string
		want  []string
	}{
		{"", []string{"#gophers", "#eco", "#100%", "#quiet"}},
		{"eco", []string{"#gophers", "#eco"}}, // topic match counts, busiest first
		{"quiet", []string{"#quiet"}},
		{"%", []string{"#100%"}}, // a literal percent, not "everything"
		{"_", nil},               // a literal underscore, not "any character"
		{"nothing here", nil},
	}
	for _, tc := range cases {
		t.Run(tc.match, func(t *testing.T) {
			got, err := db.Channels(ctx, tc.match, 50)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, c := range got {
				names = append(names, c.Name)
			}
			if !equalStrings(names, tc.want) {
				t.Errorf("Channels(%q) = %v, want %v", tc.match, names, tc.want)
			}
		})
	}

	if err := db.ClearChannels(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := db.Channels(ctx, "", 10); err != nil || len(got) != 0 {
		t.Errorf("after ClearChannels the cache holds %d rows (%v)", len(got), err)
	}
	if when, err := db.ListFetchedAt(ctx); err != nil || !when.IsZero() {
		t.Errorf("an empty cache reports a fetch time of %s (%v)", when, err)
	}
	if err := db.PutChannels(ctx, nil, epoch); err != nil {
		t.Errorf("PutChannels with nothing to store: %v", err)
	}
}
