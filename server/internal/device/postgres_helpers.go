package device

import "database/sql"

func checkAffected(res sql.Result, execErr error, notFound error) error {
	if execErr != nil {
		return execErr
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}
