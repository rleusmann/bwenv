//go:build !darwin || !cgo

package credstore

// stubStore ist die Implementierung für Plattformen ohne Touch ID.
type stubStore struct{}

// New liefert den plattformspezifischen Store.
func New() Store { return stubStore{} }

func (stubStore) Available() bool                 { return false }
func (stubStore) Enrolled() bool                  { return false }
func (stubStore) Enroll(string) error             { return ErrUnavailable }
func (stubStore) Retrieve(string) (string, error) { return "", ErrUnavailable }
func (stubStore) Erase() error                    { return ErrUnavailable }
