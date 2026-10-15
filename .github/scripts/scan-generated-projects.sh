#!/usr/bin/env bash
# Generates one project per supported architecture and database, adds the
# optional modules, and scans each with govulncheck. Generated projects pin
# their own dependencies, so a vulnerable pin would otherwise reach every user.
set -euo pipefail

got_binary="$(mktemp -d)/got"
go build -o "$got_binary" .
work="$(mktemp -d)"
trap 'rm -rf "$work" "$(dirname "$got_binary")"' EXIT
cd "$work"

scan() {
  echo "::group::govulncheck $1"
  (cd "$1" && govulncheck ./...)
  echo "::endgroup::"
}

for database in pg mysql mssql; do
  "$got_binary" init --no-input --name "standard-$database" --module "example.com/standard-$database" --architecture standard --db "$database"
  scan "standard-$database"
done

"$got_binary" init --no-input --name microservice --module example.com/microservice --architecture microservice --db pg
(cd microservice && "$got_binary" add swagger && "$got_binary" add upload && "$got_binary" add auth --jwt)
scan microservice

"$got_binary" init --no-input --name hexagonal --module example.com/hexagonal --architecture hexagonal --db pg
(cd hexagonal && "$got_binary" add swagger && "$got_binary" add auth --jwt)
scan hexagonal

"$got_binary" init workspace --no-input --name workspace --module example.com/workspace --services order --db pg
scan workspace/libs
scan workspace/services/order-service
