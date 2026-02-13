package firehose

// jetstreamEvent is the raw JSON structure from Jetstream.
type jetstreamEvent struct {
	DID    string           `json:"did"`
	TimeUS int64            `json:"time_us"`
	Kind   string           `json:"kind"`
	Commit *jetstreamCommit `json:"commit,omitempty"`
}

// jetstreamCommit is the raw commit data from Jetstream.
type jetstreamCommit struct {
	Rev        string      `json:"rev"`
	Operation  string      `json:"operation"`
	Collection string      `json:"collection"`
	RKey       string      `json:"rkey"`
	Record     *postRecord `json:"record,omitempty"`
	CID        string      `json:"cid"`
}

// postRecord is the parsed content of an app.bsky.feed.post record.
type postRecord struct {
	Type      string    `json:"$type"`
	Text      string    `json:"text"`
	CreatedAt string    `json:"createdAt"`
	Langs     []string  `json:"langs"`
	Reply     *replyRef `json:"reply,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// replyRef contains references to the parent and root of a reply chain.
type replyRef struct {
	Root   strongRef `json:"root"`
	Parent strongRef `json:"parent"`
}

// strongRef is a reference to a specific version of a record.
type strongRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}
