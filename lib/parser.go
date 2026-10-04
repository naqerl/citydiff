package lib

// Parser turns source bytes into language-agnostic entities.
// Language implementations live under lib/parser.
type Parser interface {
	Parse(src []byte) ([]Entity, error)
}
