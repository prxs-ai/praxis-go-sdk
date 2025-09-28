# DID/WebVH End-to-End Manual

Ниже — полный пошаговый сценарий, по которому можно развернуть Praxis Go Agent, сгенерировать/подписать карточку, проверить DID (`did:web`, `did:webvh`) и убедиться, что P2P-обмен карточками работает в Docker-окружении. Инструкция рассчитана на чистую систему и сопровождается командами `curl`, `go`, `docker compose` и утилитой `resolvercheck`.

---

## 0. Предусловия

1. **Софт:**
   - Go ≥ 1.23 (для сборки и тестов): `go version`
   - Docker ≥ 24 и Docker Compose Plugin ≥ 2.20: `docker version`, `docker compose version`
   - Утилиты `curl`, `jq`, `python3`
2. **Порты:** свободны `8000`, `8001`, `8090`, `8091`, `4001`, `4002`, `9000`, `9001`.
3. **Репозиторий:** склонирован в `~/Desktop/PRXS_DEPLOY/praxis-go-sdk_did`.
4. **API ключи-заглушки:**
   ```bash
   export OPENAI_API_KEY=dummy
   ```

---

1. Остановите возможные предыдущие агенты:

```bash
   pkill -f praxis-agent || true
   docker compose down --remove-orphans || true
```

2. Очистите старые ключи, если хотите всё переиздать с нуля:
   ```bash
   rm -f configs/keys/ed25519.key
   ```
3. Убедитесь, что конфиг `configs/agent.yaml` содержит блок `identity`:
   ```yaml
   agent:
     identity:
       did: "did:web:localhost%3A8000"
       did_doc_uri: "http://localhost:8000/.well-known/did.json"
       key:
         type: "ed25519"
         source: "file"
         path: "./configs/keys/ed25519.key"
         id: "key-1"
     security:
       sign_cards: true
       verify_peer_cards: true
       sign_a2a: false
       verify_a2a: false
     did_cache_ttl: "60s"
   ```

---

## 2. Локальная проверка (без Docker)

### 2.1 Запуск агента из исходников

```bash
go run ./agent --config configs/agent.yaml > /tmp/praxis-agent.log 2>&1 &
AGENT_PID=$!
sleep 3
```

### 2.2 Проверка эндпоинтов

```bash
curl -s http://localhost:8000/.well-known/did.json | jq
curl -s http://localhost:8000/.well-known/agent-card.json | jq
```

Ожидаем:

- `@context` содержит `https://www.w3.org/ns/did/v1` и `https://w3id.org/security/suites/ed25519-2020/v1`.
- В карточке присутствует массив `signatures`.

### 2.3 Проверка `did:webvh`

```bash
CANONICAL_DOC=$(curl -s http://localhost:8000/.well-known/did.json | python3 - <<'PY'
import json, sys
print(json.dumps(json.load(sys.stdin), separators=(',', ':'), sort_keys=True))
PY
)
HASH=$(printf '%s' "$CANONICAL_DOC" | python3 - <<'PY'
import sys, hashlib, base64
payload=sys.stdin.buffer.read()
print(base64.b32encode(hashlib.sha256(payload).digest()).decode().rstrip('=').lower())
PY
)
DID_WEBVH="did:webvh:localhost%3A8000:sha256-$HASH"

go run ./cmd/tools/resolvercheck --did "$DID_WEBVH" --allow-insecure
```

Должен появиться блок `WebVH verification details` с тем же хэшем.

### 2.4 Проверка подписи карточки

```bash
curl -s http://localhost:8000/.well-known/agent-card.json > /tmp/agent-card.json
python3 - <<'PY'
import json, base64
card=json.load(open('/tmp/agent-card.json'))
print(json.dumps(card['signatures'], indent=2))
PY
```

Поля `protected` и `signature` должны присутствовать.

### 2.5 Остановка локального агента

```bash
kill $AGENT_PID
```

---

## 3. Убедиться, что тесты проходят

```bash
go test ./...
```

---

## 4. Docker Compose запускает два агента

### 4.1 Сборка и запуск

```bash
docker compose build
docker compose up -d
```

Проверить статус:

```bash
docker compose ps
```

Оба контейнера должны быть `Up (healthy)`.

### 4.2 Проверка DID и карточек из Docker

```bash
curl -s http://localhost:8000/.well-known/did.json | jq
curl -s http://localhost:8000/.well-known/agent-card.json | jq '.signatures'
# для второго агента
curl -s http://localhost:8001/.well-known/did.json | jq
curl -s http://localhost:8001/.well-known/agent-card.json | jq '.signatures'
```

### 4.3 Проверка P2P-обмена

```bash
docker compose logs praxis-agent-1 | grep "Received card"
docker compose logs praxis-agent-1 | grep "🔐 DID"
docker compose logs praxis-agent-1 | grep "Rejected"  # строк быть не должно
```

### 4.4 Проверка `did:webvh` внутри Docker

```bash
curl -s http://localhost:8000/.well-known/did.json > /tmp/docker-did.json
HASH=$(python3 - <<'PY'
import json, hashlib, base64
with open('/tmp/docker-did.json') as f:
    obj=json.load(f)
canon=json.dumps(obj, separators=(',', ':'), sort_keys=True)
print(base64.b32encode(hashlib.sha256(canon.encode()).digest()).decode().rstrip('=').lower())
PY
)
DID_WEBVH="did:webvh:localhost%3A8000:sha256-$HASH"
go run ./cmd/tools/resolvercheck --did "$DID_WEBVH" --allow-insecure
```

### 4.5 Ручная проверка карточки на подпись (опционально)

```bash
docker compose exec praxis-agent-1 sh -c '
  curl -s http://localhost:8000/.well-known/agent-card.json > /tmp/card.json
  cat /tmp/card.json
'
```

### 4.6 Остановка стенда

```bash
docker compose down
```

---

## 5. Проверка валидации через внешний валидатор (например, Civic, Spruce и т.д.)

1. Извлеките DID-документ:
   ```bash
   curl -s http://localhost:8000/.well-known/did.json > did.json
   ```
2. Отправьте `did.json` в онлайн-валидатор или CLI. Важно: валидатор должен увидеть оба контекста и тип `Ed25519VerificationKey2020`.

---

## 6. Итоговая проверка перед сдачей

1. `go test ./...`
2. `docker compose build`
3. `docker compose up -d`
4. `curl http://localhost:8000/.well-known/did.json | jq`
5. `curl http://localhost:8000/.well-known/agent-card.json | jq '.signatures'`
6. `go run ./cmd/tools/resolvercheck --did "did:webvh:localhost%3A8000:sha256-…" --allow-insecure`
7. `docker compose down`
