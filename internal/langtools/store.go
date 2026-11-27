package langtools

import (
	"context"
	"time"

	"pelokalan/pkg/thesaurus"
)

// ThesaurusRepository stores the thesaurus
type ThesaurusRepository interface {
	Count(ctx context.Context) (int, error)
	All(ctx context.Context) ([]thesaurus.Entry, error)
	// ReplaceAll swaps the whole thesaurus in one transaction
	ReplaceAll(ctx context.Context, entries []thesaurus.Entry) error
}

// ExtraWord is a word a project accepts in addition to the spelling word list
type ExtraWord struct {
	Word      string
	AddedBy   string // username ("" when unknown)
	CreatedAt time.Time
}

// SpellingRepository stores the spelling word list and each project's extra words
type SpellingRepository interface {
	CountWords(ctx context.Context) (int, error)
	Words(ctx context.Context) ([]string, error)
	// ReplaceWords swaps the whole word list in one transaction
	ReplaceWords(ctx context.Context, words []string) error
	ExtraWords(ctx context.Context, projectID int64) ([]ExtraWord, error)
	AddExtraWord(ctx context.Context, projectID int64, word string, userID int64) error
	DeleteExtraWord(ctx context.Context, projectID int64, word string) error
}
