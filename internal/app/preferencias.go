package app

import "database/sql"

// Pref lê uma preferência do banco ("" quando não existe).
func Pref(conn dbtx, chave string) (string, error) {
	var v string
	err := conn.QueryRow(`SELECT valor FROM preferencias WHERE chave = ?`, chave).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetPref grava (ou troca) uma preferência; valor "" apaga.
func SetPref(conn dbtx, chave, valor string) error {
	if valor == "" {
		_, err := conn.Exec(`DELETE FROM preferencias WHERE chave = ?`, chave)
		return err
	}
	_, err := conn.Exec(`INSERT INTO preferencias (chave, valor) VALUES (?, ?)
		ON CONFLICT (chave) DO UPDATE SET valor = excluded.valor`, chave, valor)
	return err
}
