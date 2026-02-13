package domain

import "time"

// Post represents an indexed BlueSky post stored in our database.
type Post struct {
	// URI is the AT-URI of the post (e.g. at://did:plc:abc/app.bsky.feed.post/3l3qo2vuowo2b).
	URI string

	// CID is the content identifier of the record.
	CID string

	// IndexedAt is when we indexed this post.
	IndexedAt time.Time
}

// IncomingPost represents a new post from the firehose that hasn't been
// persisted yet. It carries the text and metadata needed for matching.
type IncomingPost struct {
	// URI is the AT-URI of the post.
	URI string

	// CID is the content identifier of the record.
	CID string

	// AuthorDID is the DID of the post's author.
	AuthorDID string

	// Text is the post body text used for keyword matching.
	Text string

	// Langs is the list of language tags set by the author's client.
	Langs []string
}
