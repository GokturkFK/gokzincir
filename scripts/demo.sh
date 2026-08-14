#!/usr/bin/env bash
# GÖKZİNCİR uçtan uca demo — DoD madde 1-4'ü ÇALIŞAN sistem üzerinde gösterir.
#
#   1) envanter doldurulur, ilişki grafiği kurulur      (DoD 1)
#   2) bir NHI için blast-radius API'den okunur          (DoD 2)
#   3) MEŞRU bir NHI kullanılır  -> alarm YOK             (DoD 4, sıfır-FP)
#   4) ekilen SAHTE NHI kullanılır -> Critical alarm      (DoD 3)
#
# Kullanım:
#   make docker-up          # ayri bir terminalde
#   ./scripts/demo.sh
#
# Ortam:
#   API=http://localhost:8100   (varsayilan)
set -euo pipefail

API="${API:-http://localhost:8100}"
COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker/docker-compose.yml}"

bold() { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✔\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✘ %s\033[0m\n' "$*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || fail "$1 gerekli"; }
need curl

bold "0) Servis hazir mi"
for _ in $(seq 1 30); do
	if curl -fsS "$API/healthz" >/dev/null 2>&1; then break; fi
	sleep 1
done
curl -fsS "$API/healthz" >/dev/null || fail "$API ayakta degil (make docker-up calisiyor mu?)"
ok "$API saglikli"

bold "1) Envanter + iliski grafigi (DoD 1)"
# Kucuk ama gercekci bir topoloji: ci-runner -> deploy-bot -> prod-db-reader
# (yanal hareket zinciri) + iliskisiz bir kayit.
curl -fsS -X POST "$API/api/v1/inventory" -H 'Content-Type: application/json' -d '{
  "identities": [
    {"id":"11111111-1111-4111-8111-111111111111","type":"service_account","owner":"platform","scope":"ci:run"},
    {"id":"22222222-2222-4222-8222-222222222222","type":"service_account","owner":"platform","scope":"deploy"},
    {"id":"33333333-3333-4333-8333-333333333333","type":"token","owner":"data","scope":"db:read"},
    {"id":"44444444-4444-4444-8444-444444444444","type":"machine_identity","owner":"billing","scope":"invoice:read"}
  ],
  "edges": [
    {"from":"11111111-1111-4111-8111-111111111111","to":"22222222-2222-4222-8222-222222222222"},
    {"from":"22222222-2222-4222-8222-222222222222","to":"33333333-3333-4333-8333-333333333333"}
  ]
}' | tee /dev/stderr >/dev/null
ok "4 kimlik + 2 kenar yazildi (idempotent — tekrar calistirmak cift kayit uretmez)"

bold "2) Blast-radius (DoD 2)"
echo "  ci-runner ele gecerse nereye ulasilir?"
curl -fsS "$API/api/v1/nhi/11111111-1111-4111-8111-111111111111/blast-radius"
echo
ok "hasar yuzeyi API'den okunabiliyor (bu bir ALARM DEGIL — salt gorunurluk)"

bold "3) MESRU NHI kullanimi (DoD 4 — sifir-FP)"
legit=$(curl -fsS -X POST "$API/api/v1/nhi-usage" -H 'Content-Type: application/json' \
	-d '{"NHIID":"33333333-3333-4333-8333-333333333333","AccessedBy":"prod-app-01","TargetNodeID":"db-primary"}')
echo "  yanit: $legit"
case "$legit" in
	*'"triggered":false'*) ok "alarm YOK — envanterdeki gercek NHI'nin mesru kullanimi sessiz" ;;
	*) fail "mesru kullanim alarm uretti: $legit" ;;
esac

bold "4) SAHTE NHI kullanimi (DoD 3)"
# Tuzagin id'si hicbir API ucundan donmez (bkz. internal/seed) — operator
# onu YALNIZCA kendi log'undan ogrenir. Saldirgan icin bu id, envanterin
# icinde gercek bir kayittan ayirt edilemez duran satirdir.
decoy="${DECOY_ID:-}"
if [ -z "$decoy" ]; then
	decoy=$(docker compose -f "$COMPOSE_FILE" logs gokzincir 2>/dev/null \
		| grep -o '"nhi_id":"[^"]*"' | tail -1 | cut -d'"' -f4 || true)
fi
[ -n "$decoy" ] || fail "ekilen tuzak bulunamadi (DECOY_ID=... ile elle verebilirsin)"
echo "  tuzak: $decoy"

for i in 1 2; do
	trip=$(curl -fsS -X POST "$API/api/v1/nhi-usage" -H 'Content-Type: application/json' \
		-d "{\"NHIID\":\"$decoy\",\"AccessedBy\":\"attacker-01\",\"TargetNodeID\":\"secrets-vault\"}")
	echo "  $i. dokunus: $trip"
done
ok "tuzak tetiklendi"

bold "5) Alarmlar (panelin de okudugu sozlesme)"
curl -fsS "$API/api/v1/alerts"
echo
cat <<'EOT'

  Beklenen: attacker-01 icin TEK bir Critical alarm (iki dokunus ayri iki
  alarm degil, ayni kampanya olarak birlesir), technique = T1078.004.

  Panelde gormek icin (gokturk-deception-mesh):
    ALERT_SOURCES="GÖKTÜRK=http://control-api:8080,GÖKZİNCİR=http://host.docker.internal:8100"
EOT
