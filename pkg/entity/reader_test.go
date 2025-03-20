package entity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntityReader(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity

	r := require.New(t)

	e, err := store.CreateEntity(t.Context(), Attrs(
		Named("test/person"),
		Doc, "A test person",
	))
	r.NoError(err)

	// Define a struct to read into
	var person struct {
		Doc string `entity:"db/doc"`
	}

	// Read the entity info into the struct
	err = e.ReadInfo(&person)
	r.NoError(err)

	// Validate the struct values
	r.Equal("A test person", person.Doc)

}
