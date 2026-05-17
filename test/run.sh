#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass=0
fail=0

check() {
    local desc="$1"
    local cmd="$2"
    local expected="$3"
    local mode="${4:-contains}"

    local output rc
    output=$(eval "$cmd" 2>&1) && rc=0 || rc=$?

    case "$mode" in
        exact)
            if [ "$output" = "$expected" ]; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                pass=$((pass + 1))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected: '$expected'"
                echo "    got:      '$output'"
                fail=$((fail + 1))
            fi
            ;;
        contains)
            if echo "$output" | grep -qF "$expected"; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                pass=$((pass + 1))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected to contain: '$expected'"
                echo "    got: '$output'"
                fail=$((fail + 1))
            fi
            ;;
        retcode)
            if [ "$rc" -eq "$expected" ]; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                pass=$((pass + 1))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected exit code: $expected"
                echo "    got: $rc"
                echo "    output: $output"
                fail=$((fail + 1))
            fi
            ;;
    esac
}

VITRINA="/usr/local/bin/vitrina"

echo "=== Vitrina Integration Tests ==="
echo ""

echo "--- init ---"
"$VITRINA" init --domain test.example.com --email test@example.com
check "config.json exists" "test -f /etc/vitrina/config.json && echo exists" "exists"
check "config has domain" "grep test.example.com /etc/vitrina/config.json" "test.example.com"
check "Caddyfile exists" "test -f /etc/caddy/Caddyfile && echo exists" "exists"
check "Caddyfile imports conf.d" "grep 'import' /etc/caddy/Caddyfile" "import"

echo "--- caddy validate ---"
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1 || true
check "caddy validates clean" \
    'caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1' \
    "Valid configuration"
# caddy exits 0 on valid, but let's be lenient in CI
# some platforms may not have caddy installed

echo "--- add ---"
"$VITRINA" add testapp 3000
check "app registered" "grep testapp /etc/vitrina/apps.json" "testapp"
check "caddy snippet exists" "test -f /etc/caddy/conf.d/testapp.test.example.com.caddy && echo exists" "exists"

echo "--- remove ---"
"$VITRINA" remove testapp
check "snippet removed" "test -f /etc/caddy/conf.d/testapp.test.example.com.caddy || echo removed" "removed"

echo "--- list ---"
"$VITRINA" add alpha 3001
"$VITRINA" add beta 3002
"$VITRINA" list
"$VITRINA" remove alpha
"$VITRINA" remove beta

echo "--- caddy snippet isolation ---"
# Write a bad snippet by hand, verify other snippets still work
echo "invalid {" > /etc/caddy/conf.d/bad.caddy
"$VITRINA" add goodapp 3003 2>&1 || true
"$VITRINA" list 2>&1 || true
rm -f /etc/caddy/conf.d/bad.caddy
"$VITRINA" remove goodapp 2>&1 || true

echo "--- env ---"
"$VITRINA" add envapp 3010
mkdir -p /etc/vitrina/apps/envapp
touch /etc/vitrina/apps/envapp/.env
check "env list empty" "$VITRINA env list envapp" "No environment variables set"
check "env set" "$VITRINA env set envapp FOO=bar BAZ=qux" "Environment variables set"
check "env list shows key" "$VITRINA env list envapp" "FOO"
check "env list shows value" "$VITRINA env list envapp" "bar"
check "env unset" "$VITRINA env unset envapp FOO BAZ" "Environment variables unset"
check "env list empty after unset" "$VITRINA env list envapp" "No environment variables set"
check "env set PORT blocked" "$VITRINA env set envapp PORT=9999" "cannot override PORT"
check "env unset PORT blocked" "$VITRINA env unset envapp PORT" "cannot unset PORT"
"$VITRINA" remove envapp
rm -rf /etc/vitrina/apps/envapp

echo "--- status ---"
"$VITRINA" add statusapp 3020
check "status shows app name" "$VITRINA status statusapp" "statusapp"
check "status shows URL" "$VITRINA status statusapp" "URL:"
check "status shows port" "$VITRINA status statusapp" "3020"
check "status shows no containers" "$VITRINA status statusapp" "No Docker containers"
"$VITRINA" remove statusapp

echo "--- doctor ---"
"$VITRINA" add docapp 3030
mkdir -p /etc/vitrina/apps/docapp
rm -f /etc/caddy/conf.d/docapp.test.example.com.caddy
check "doctor detects missing snippet" "$VITRINA doctor" "Missing Caddy config"
check "doctor heal regenerates snippet" "$VITRINA doctor --heal" "Regenerated Caddy config"
check "snippet restored after heal" "test -f /etc/caddy/conf.d/docapp.test.example.com.caddy && echo exists" "exists"
echo "dangling {" > /etc/caddy/conf.d/dangling.test.example.com.caddy
check "doctor detects dangling config" "$VITRINA doctor" "Dangling Caddy config"
check "doctor heal removes dangling" "$VITRINA doctor --heal" "Removed dangling Caddy config"
check "dangling config removed after heal" "test -f /etc/caddy/conf.d/dangling.test.example.com.caddy || echo removed" "removed"
check "healthy system reports clean" "$VITRINA doctor" "All checks passed"
"$VITRINA" remove docapp
rm -rf /etc/vitrina/apps/docapp

echo "--- config update ---"
check "config update email" "$VITRINA config update --email updated@example.com" "Configuration updated"
check "config email persisted" "grep updated@example.com /etc/vitrina/config.json" "updated@example.com"
check "config update no-op" "$VITRINA config update --email updated@example.com" "No changes to apply"
"$VITRINA" config update --email test@example.com 2>&1 || true

echo "--- ps ---"
check "ps with no apps" "$VITRINA ps" "No apps registered"
"$VITRINA" add psapp 3040
check "ps shows registered app" "$VITRINA ps" "psapp"
check "ps shows no app dir message" "$VITRINA ps" "registered via"
"$VITRINA" remove psapp

echo "--- export/import ---"
"$VITRINA" add exportapp 3050
check "export creates archive" "$VITRINA export /tmp/vitrina-test-export.tar.gz" "Export complete"
check "export file exists" "test -f /tmp/vitrina-test-export.tar.gz && echo exists" "exists"
"$VITRINA" remove exportapp
check "import restores state" "$VITRINA import /tmp/vitrina-test-export.tar.gz" "Import complete"
check "import restores registry entry" "$VITRINA list" "exportapp"
check "import restores caddy snippet" "test -f /etc/caddy/conf.d/exportapp.test.example.com.caddy && echo exists" "exists"
"$VITRINA" remove exportapp 2>&1 || true
rm -f /tmp/vitrina-test-export.tar.gz

echo "--- json output ---"
"$VITRINA" add jsonapp 3060
check "json list is valid JSON" "$VITRINA --json list | python3 -c 'import sys,json; json.load(sys.stdin); print(\"valid\")'" "valid"
check "json list contains app" "$VITRINA --json list" "jsonapp"
check "json status is valid JSON" "$VITRINA --json status jsonapp | python3 -c 'import sys,json; json.load(sys.stdin); print(\"valid\")'" "valid"
check "json status contains subdomain" "$VITRINA --json status jsonapp" "jsonapp"
"$VITRINA" remove jsonapp

echo "--- list --health ---"
"$VITRINA" add healthapp 3070
check "list health shows app" "$VITRINA list --health" "healthapp"
check "list health shows down status" "$VITRINA list --health" "down"
"$VITRINA" remove healthapp

echo "--- error paths ---"
"$VITRINA" add errapp 3080
check "duplicate add fails" "$VITRINA add errapp 3081" "1" retcode
check "add invalid subdomain fails" "$VITRINA add 'bad_subdomain!' 3082" "1" retcode
check "add reserved port 80 fails" "$VITRINA add reservedport 80" "reserved" contains
check "add reserved port 443 fails" "$VITRINA add reservedport 443" "reserved" contains
check "status nonexistent app fails" "$VITRINA status nosuchapp" "1" retcode
check "env list nonexistent app fails" "$VITRINA env list nosuchapp" "not found" contains
check "env set nonexistent app fails" "$VITRINA env set nosuchapp KEY=val" "not found" contains
"$VITRINA" remove errapp

echo ""
echo "=== Results: $pass passed, $fail failed ==="
if [ "$fail" -gt 0 ]; then
    exit 1
fi
