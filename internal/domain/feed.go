package domain

// FeedSkeleton is the response body for getFeedSkeleton.
type FeedSkeleton struct {
	Cursor string
	Posts  []SkeletonPost
}

// SkeletonPost is a single entry in a feed skeleton.
type SkeletonPost struct {
	// Post is the AT-URI of the post.
	Post string
}

// FeedDescription describes a single feed served by this generator.
type FeedDescription struct {
	// URI is the AT-URI of the feed generator record.
	URI string
}

// GeneratorDescription is the response body for describeFeedGenerator.
type GeneratorDescription struct {
	DID   string
	Feeds []FeedDescription
}
