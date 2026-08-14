#!/bin/sh
# Compose ayaga kalkarken migration'lari uygular (tek seferlik servis).
#
# NEDEN VAR: `make docker-up` calisan bir stack veriyordu ama SEMASIZ —
# uygulama her sorguda "relation does not exist" ile patliyordu. Semayi
# host'tan `make migrate-up` ile uygulamak goose'un kurulu olmasini
# gerektiriyor; demoyu izleyen/kuran birinin Go tooling'i olmayabilir
# (air-gapped kurulumda da yok).
#
# NEDEN YENI BIR IMAJ DEGIL: postgres:16-alpine zaten stack'te var, psql
# onun icinde. Bir goose imaji eklemek internet + yeni bir tedarik zinciri
# yuzeyi demekti (bkz. PROJECT_PLAN.md bol. 7: gereklilik olculmeden
# bagimlilik eklenmez).
set -eu

PGHOST="${PGHOST:-postgres}"
PGUSER="${POSTGRES_USER:-gokzincir}"
PGDATABASE="${POSTGRES_DB:-gokzincir}"
PGPASSWORD="${POSTGRES_PASSWORD:-gokzincir}"
export PGPASSWORD

psql_cmd() {
	psql -v ON_ERROR_STOP=1 -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" -q "$@"
}

# goose'un KENDI surum tablosu kullaniliyor. Ayri bir tablo tutsaydik,
# sonradan host'tan `make migrate-up` calistiran biri her seyi ikinci kez
# uygulamaya calisir ve "already exists" ile patlardi.
psql_cmd -c "CREATE TABLE IF NOT EXISTS goose_db_version (
	id serial PRIMARY KEY,
	version_id bigint NOT NULL,
	is_applied boolean NOT NULL,
	tstamp timestamp NULL DEFAULT now()
);"
psql_cmd -c "INSERT INTO goose_db_version (version_id, is_applied)
	SELECT 0, true WHERE NOT EXISTS (SELECT 1 FROM goose_db_version WHERE version_id = 0);"

for file in /migrations/*.sql; do
	[ -e "$file" ] || continue
	version=$(basename "$file" | cut -d_ -f1 | sed 's/^0*//')
	version=${version:-0}

	applied=$(psql_cmd -tA -c "SELECT count(*) FROM goose_db_version
		WHERE version_id = $version AND is_applied")
	if [ "$applied" != "0" ]; then
		echo "atlandi (zaten uygulanmis): $(basename "$file")"
		continue
	fi

	echo "uygulaniyor: $(basename "$file")"
	# Yalnizca +goose Up bolumu calistirilir; dosyanin tamamini psql'e
	# vermek Down bolumundeki DROP'lari da calistirir ve yeni yazilan
	# semayi ayni anda silerdi.
	# --single-transaction: dosya ya tamamen uygulanir ya hic — yarim
	# uygulanmis bir migration, surum tablosunda "uygulanmadi" gorunurken
	# tablolarin bir kismini birakirdi.
	sed -n '/^-- +goose Up/,/^-- +goose Down/p' "$file" \
		| sed '/^-- +goose Down/d' \
		| psql_cmd --single-transaction

	psql_cmd -c "INSERT INTO goose_db_version (version_id, is_applied)
		VALUES ($version, true);"
done

echo "migration'lar tamam"
