package store

import "errors"

// AdminPassword is the administrator's bcrypt hash and its CAS revision.
type AdminPassword struct {
	Hash     []byte
	Revision int64
}

// AdminPassword returns ErrNotFound before the first administrator exists.
func (s *Store) AdminPassword() (AdminPassword, error) {
	var out AdminPassword
	err := s.read.QueryRow(`SELECT hash,revision FROM admin WHERE id=1`).Scan(&out.Hash, &out.Revision)
	return out, err
}

// CreateAdminPassword stores the first administrator password at revision 1.
// It returns ErrConflict when an administrator already exists.
func (s *Store) CreateAdminPassword(hash []byte) (AdminPassword, error) {
	if len(hash) == 0 {
		return AdminPassword{}, errors.New("administrator password hash is required")
	}
	r, err := s.db.Exec(`INSERT INTO admin(id,hash,revision) VALUES(1,?,1) ON CONFLICT(id) DO NOTHING`, hash)
	if err != nil {
		return AdminPassword{}, err
	}
	if n, err := r.RowsAffected(); err != nil {
		return AdminPassword{}, err
	} else if n != 1 {
		return AdminPassword{}, ErrConflict
	}
	return AdminPassword{Hash: hash, Revision: 1}, nil
}

// ReplaceAdminPassword stores hash when the revision is still expected and
// returns the new revision; otherwise it returns ErrConflict.
func (s *Store) ReplaceAdminPassword(hash []byte, expected int64) (AdminPassword, error) {
	if len(hash) == 0 {
		return AdminPassword{}, errors.New("administrator password hash is required")
	}
	r, err := s.db.Exec(`UPDATE admin SET hash=?,revision=revision+1 WHERE id=1 AND revision=?`, hash, expected)
	if err != nil {
		return AdminPassword{}, err
	}
	if n, err := r.RowsAffected(); err != nil {
		return AdminPassword{}, err
	} else if n != 1 {
		return AdminPassword{}, ErrConflict
	}
	return AdminPassword{Hash: hash, Revision: expected + 1}, nil
}
