package store

import (
	"laz/internal/server/model"
	"time"
)

func (s *SQLStore) PutHysteriaFileSet(v model.HysteriaFileSet) error {
	_, err := s.exec(`insert into hysteria_masquerade_filesets(id,archive_encrypted,manifest_json,created_at_ms) values(?,?,?,?) on conflict(id) do nothing`, v.ID, v.Archive, v.ManifestJSON, time.Now().UnixMilli())
	return err
}
func (s *SQLStore) GetHysteriaFileSet(id string) (model.HysteriaFileSet, error) {
	var v model.HysteriaFileSet
	err := s.queryRow(`select id,archive_encrypted,manifest_json from hysteria_masquerade_filesets where id=?`, id).Scan(&v.ID, &v.Archive, &v.ManifestJSON)
	return v, convertErr(err)
}
func (s *SQLStore) GetHysteriaFileSetManifest(id string) (string, error) {
	var manifest string
	err := s.queryRow(`select manifest_json from hysteria_masquerade_filesets where id=?`, id).Scan(&manifest)
	return manifest, convertErr(err)
}
func (s *SQLStore) PruneHysteriaFileSets(before time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`update hysteria2_node_configs set masquerade_fileset_id=null,applied_masquerade_fileset_id=null where node_id in(select id from nodes where status=?)`), model.StatusDeleted); err != nil {
		return err
	}
	if _, err = tx.Exec(s.rebind(`delete from hysteria_masquerade_filesets where created_at_ms < ? and not exists(select 1 from hysteria2_node_configs c where c.masquerade_fileset_id=hysteria_masquerade_filesets.id or c.applied_masquerade_fileset_id=hysteria_masquerade_filesets.id)`), before.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SecureStore) PutHysteriaFileSet(v model.HysteriaFileSet) error {
	v.Archive = s.encrypt(v.Archive)
	return s.inner.PutHysteriaFileSet(v)
}
func (s *SecureStore) GetHysteriaFileSet(id string) (model.HysteriaFileSet, error) {
	v, err := s.inner.GetHysteriaFileSet(id)
	v.Archive = s.decrypt(v.Archive)
	return v, err
}
func (s *SecureStore) GetHysteriaFileSetManifest(id string) (string, error) {
	return s.inner.GetHysteriaFileSetManifest(id)
}
func (s *SecureStore) PruneHysteriaFileSets(before time.Time) error {
	return s.inner.PruneHysteriaFileSets(before)
}
