#!/usr/bin/env bash
# 前端相依套件：node_modules 在、而且 pnpm-lock.yaml 與各 package.json 都沒比安裝標記新，就不重跑 pnpm install。
set -euo pipefail
cd "$(dirname "$0")/../web"
stamp=node_modules/.hg-install-stamp
need=0
[ -f "$stamp" ] || need=1
if [ "$need" = 0 ]; then
  for f in pnpm-lock.yaml pnpm-workspace.yaml package.json apps/*/package.json packages/*/package.json; do
    if [ "$f" -nt "$stamp" ]; then need=1; break; fi
  done
fi
if [ "$need" = 1 ]; then
  pnpm install --frozen-lockfile
  touch "$stamp"
fi
