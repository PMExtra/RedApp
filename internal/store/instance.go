package store

// InstanceUID is the random identity of this RedApp instance. It never
// changes for a data directory; a copied data directory shares it.
func (s *Store) InstanceUID() (string, error) {
	var uid string
	err := s.read.QueryRow(`SELECT uid FROM instance WHERE id=1`).Scan(&uid)
	return uid, err
}
