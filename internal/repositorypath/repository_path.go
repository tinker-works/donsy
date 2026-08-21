package repositorypath

import (
	"fmt"
	"strings"
)

// Encode maps a repository name to one filesystem path component. Slashes keep
// their existing readable marker, while underscores are escaped so that marker
// cannot collide with an underscore in either repository component.
func Encode(repository string) string {
	var encoded strings.Builder
	for _, character := range repository {
		switch character {
		case '/':
			encoded.WriteString("__")
		case '_':
			encoded.WriteString("_u")
		default:
			encoded.WriteRune(character)
		}
	}
	return encoded.String()
}

// Decode reverses Encode and refuses path components that do not use its
// alphabet. Repository names are persisted in the tree, so accepting an
// ambiguous spelling here would make a later read select the wrong repository.
func Decode(encoded string) (string, error) {
	var repository strings.Builder
	for index := 0; index < len(encoded); index++ {
		if encoded[index] != '_' {
			repository.WriteByte(encoded[index])
			continue
		}
		if index+1 == len(encoded) {
			return "", fmt.Errorf("repository path %q ends with an escape marker", encoded)
		}
		switch encoded[index+1] {
		case '_':
			repository.WriteByte('/')
		case 'u':
			repository.WriteByte('_')
		default:
			return "", fmt.Errorf("repository path %q has an invalid escape", encoded)
		}
		index++
	}
	return repository.String(), nil
}
