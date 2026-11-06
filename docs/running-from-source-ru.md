# Запуск v2rayA из исходников и доказательство, что всё работает

Рецепт для чистой машины: соберите эту копию репозитория, запустите её и докажите, что узел подключается, а набор правил доходит до ядра. Написан для выполнения по порядку без чтения чего-либо ещё.

Это точка входа. Для проверки одной конкретной функции на машине, где v2rayA уже работает, см. [testing-on-a-real-machine.md](testing-on-a-real-machine.md).

## Что понадобится

- **Docker.** Каждый шаг сборки ниже выполняется в контейнере; ничего не нужно устанавливать на хост.
- **Узел.** Одна share-ссылка — `vless://`, `vmess://`, `trojan://` или `ss://` — от провайдера. Значения по умолчанию нет; всё ниже зависит от неё.
- **Около пятнадцати минут.** Первая сборка скачивает зависимости.

**Root не нужен.** Прозрачный прокси по умолчанию выключен (`service/db/configure/setting.go`), поэтому v2rayA не устанавливает правил firewall, не трогает `/etc/resolv.conf` и не открывает TUN-устройство. Он только слушает loopback-порты.

## Пять портов

| Порт | Тег в конфигурации ядра | Получает правила маршрутизации |
| --- | --- | --- |
| 2017 | `api-in` | — |
| 20170 | `socks` | **нет** |
| 20171 | `http` | **нет** |
| 20172 | `rule-http` | **да** |

Эта таблица — самая частая причина, по которой набор правил «ничего не делает»: правила привязаны только к rule-портам (`service/kernel/v2ray/template_routing.go`, `setRulePortRouting`). Клиент, направленный на 20170 или 20171, никогда их не увидит. Существует также входящее подключение SOCKS5-with-rules, выключенное по умолчанию.

В сгенерированной конфигурации теги несут суффикс семейства адресов, поэтому правило выглядит как `["rule-http_ipv4", "rule-socks", "rule-http_ipv6"]`. Ориентируйтесь на префикс `rule-`, а не на точное имя.

## Шаг 1 — сборка

```bash
cd ~/v2raya-test/src

docker run --rm -v "$PWD:/repo" -w /repo/gui node:24 \
  bash -c 'corepack enable >/dev/null && yarn install --frozen-lockfile && yarn build'

mkdir -p service/server/router/web && cp -a web/. service/server/router/web/

docker run --rm -v "$PWD:/repo" -w /repo/core golang:1.27 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya_core ./main'

docker run --rm -v "$PWD:/repo" -w /repo/service golang:1.27 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya .'

ls -la v2raya v2raya_core
```

**Вы должны увидеть** два файла в списке с размерами. Если первая команда завершается с ошибкой, обычная причина — отсутствие вывода `web/`, который нужен каждому следующему шагу. Если падает сам `docker run` с ошибкой bridge-сети, смотрите вариант `--network host` в разделе 1.1 [testing-on-a-real-machine-ru.md](testing-on-a-real-machine-ru.md).

## Шаг 2 — запуск

```bash
cd ~/v2raya-test/src
./v2raya --lite > /tmp/v2raya.log 2>&1 &

sleep 5
ss -lntp | grep -E '2017|20170|20171'
```

**Вы должны увидеть** три строки, по одной на порт.

При самом первом запуске v2rayA скачивает `geoip.dat` и `geosite.dat`, вместе около 90 МБ, и API не отвечает, пока скачивание не завершится. Подождите минуту-другую, затем:

```bash
curl -s http://127.0.0.1:2017/api/version | head -c 200; echo
```

**Если ничего не отвечает**, читайте `/tmp/v2raya.log` — причина в первых строках. Флаг `--lite` подразумевается, когда запуск не от root; здесь он указан, чтобы намерение было зафиксировано.

## Шаг 3 — учётная запись, и сохраните токен

На свежей установке учётных записей нет, поэтому API отвечает `first: true` и отказывает во всём остальном, пока запись не создана.

```bash
curl -s -X POST http://127.0.0.1:2017/api/account \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"test1234"}'
```

Каждый следующий запрос нуждается в токене. Сохраните его, чтобы никогда не вставлять вручную:

```bash
curl -s -X POST http://127.0.0.1:2017/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"test1234"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])' > /tmp/token

export TOKEN=$(cat /tmp/token)
echo "token: ${TOKEN:0:20}..."
```

**Вы должны увидеть** начало длинной строки. `TOKEN` живёт только в этом шелле; в новом терминале выполните `export TOKEN=$(cat /tmp/token)` снова.

## Шаг 4 — добавьте узел и подключите его

Подставьте свою ссылку вместо `<LINK>`. Поле — `url`, в единственном числе:

```bash
curl -s -X POST http://127.0.0.1:2017/api/import \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"<LINK>"}' > /dev/null

SERVER_ID=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["touch"]["servers"][0]["id"])')
echo "node id: $SERVER_ID"
```

Выбор узла — отдельный вызов, и он должен произойти до запуска ядра — `StartV2ray` отказывает с `no server is selected`. **Этот вызов также запускает ядро**, так что следующий после него вызов лишь подтверждает состояние:

```bash
curl -s -X POST http://127.0.0.1:2017/api/connection \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"_type\":\"server\",\"id\":$SERVER_ID,\"sub\":0,\"outbound\":\"proxy\"}" > /dev/null

curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; c=json.load(sys.stdin)["data"]["touch"]["connectedServer"]; print("connected:", c)'
```

**Вы должны увидеть** `connected: [{'_type': 'server', ...}]`.

Этот запрос не принимает **тела**:

```bash
curl -s -X POST http://127.0.0.1:2017/api/v2ray -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys; print("running:", json.load(sys.stdin)["data"]["running"])'
```

**Вы должны увидеть** `running: True`.

## Шаг 5 — работает ли узел вообще?

Этот шаг — не необязательный. Всё после него измеряет маршрутизацию, а сломанный узел выглядит в точности как сломанный набор правил.

```bash
echo "A, direct:  $(curl -s --max-time 15 https://ifconfig.me)"
echo "B, through: $(curl -s --max-time 15 --socks5-hostname 127.0.0.1:20170 https://ifconfig.me)"
```

**A и B должны различаться.** Если они равны, узел не несёт трафик и ни один дальнейший результат ничего не значит.

`--socks5-hostname` обязателен. Без него curl разрешает имя локально и отправляет адрес, поэтому правила `geosite:` никогда не смогут совпасть.

Запомните A и B: каждая проверка с этого момента — это «это дало A или B?».

## Шаг 6 — докажите, что механизм маршрутизации работает

Сделайте это с правилом, исход которого вам уже известен, прежде чем импортировать что-либо опубликованное. Отлаживать плохое подключение и плохую конвертацию одновременно гораздо сложнее, чем доказывать каждое по очереди.

```bash
cat > /tmp/rule.txt <<'EOF'
# control: example.com goes out directly, everything else through the proxy
domain(example.com)->direct
default: proxy
EOF

python3 -c 'import json;print(json.dumps({"routingA":open("/tmp/rule.txt").read()}))' > /tmp/rule.json
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/rule.json > /dev/null
```

Включите RoutingA. Настройка — это `pacMode` в JSON, а тело запроса — сам объект настройки **целиком** — не обёрнутый в объект `{"setting": …}`. Обёртка молча игнорируется: обработчик декодирует тело поверх сохранённой настройки, неизвестный ключ ни с чем не совпадает, и `pacMode` остаётся где был. Прочитайте, измените одно поле, запишите обратно:

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["pacMode"]="routingA"
print(json.dumps(s))' > /tmp/set.json

curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/set.json
```

**Вы должны увидеть** `"code": "SUCCESS"`. `FAIL` здесь — тема шага 7, и его стоит прочитать, прежде чем идти дальше.

**Теперь проверка, которая важнее всего.** Если ни одно правило не несёт тег rule-порта, RoutingA не применился и каждое измерение ниже бессмысленно:

```bash
CFG="$HOME/.config/v2raya/config.json"
python3 - "$CFG" <<'PY'
import json,sys
rules = json.load(open(sys.argv[1])).get("routing", {}).get("rules", [])
mine = [r for r in rules
        if any(t.startswith("rule-") for t in (r.get("inboundTag") or []))]
print("rules total:", len(rules), "| on rule ports:", len(mine))
for r in mine:
    print("  ", r.get("outboundTag") or r.get("balancerTag"),
          "<-", r.get("domain") or r.get("ip"))
PY
```

**Вы должны увидеть** `on rule ports: 2`, одно `direct` и одно `proxy`.

Наконец, сравните два порта. Это доказательство, что правила применяются:

```bash
echo "20172 (expect A): $(curl -s --max-time 15 -x http://127.0.0.1:20172 https://ifconfig.me)"
echo "20170 (expect B): $(curl -s --max-time 15 --socks5-hostname 127.0.0.1:20170 https://ifconfig.me)"
```

20172 должен дать **A**, а 20170 должен дать **B**. Если оба дают B, правило не применилось.

## Шаг 6b — установите geo-файлы, нужные правилам

**Сделайте это до шага 7, иначе шаг 7 сломает сервис.**

Набор правил — это список имён вроде `geosite:ru-blocked` и `geoip:ru-blocked`. Ядро разрешает эти имена по geo-файлам в каталоге ассетов. v2rayA при первом запуске устанавливает `geoip.dat` от v2fly и `dlc.dat` от `domain-list-community`, и **ни один из них не содержит российских категорий**. Поэтому:

- импорт на шаге 7 завершается успешно и сообщает `SUCCESS`,
- правила сохраняются,
- а затем **ядро отказывается запускаться**, потому что не может разрешить `geoip:ru-blocked`. v2rayA сообщает об этом как `invalid config: the core could not restart with the new settings, the previous ones are back` и молча откатывает настройку назад.

Деталь — в логе v2rayA, а не в ответе API:

```bash
grep -i 'illegal ip rule\|Failed to start' "$(dirname "$(which v2raya)")/../.config/v2raya/v2raya.log" 2>/dev/null \
  || grep -i 'illegal ip rule\|Failed to start' /tmp/v2raya.log
```

```
illegal ip rule: geoip:ru-blocked > failed to check code RU-BLOCKED from geoip.dat > EOF
```

`EOF` там означает «прошёл весь файл и не нашёл код», а не обрезанное скачивание.

Установите подходящие файлы до импорта. Каталог ассетов — `$XDG_DATA_HOME/v2raya`, именно туда направлено ядро:

```bash
ASSETS="${XDG_DATA_HOME:-$HOME/.local/share}/v2raya"
echo "каталог ассетов: $ASSETS"

BASE='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release'
for f in geoip.dat geosite.dat; do
  [ -f "$ASSETS/$f" ] && cp "$ASSETS/$f" "$ASSETS/$f.v2fly.bak"
  curl -sSL -o "$ASSETS/$f" "$BASE/$f" && echo "$f: $(stat -c%s "$ASSETS/$f") байт"
done
```

**Вы должны увидеть** примерно 18 МБ и 74 МБ. Резервные копии называются `.v2fly.bak`; чтобы вернуться, восстановите их:

```bash
cd "$ASSETS" && for f in geoip.dat geosite.dat; do [ -f "$f.v2fly.bak" ] && mv "$f.v2fly.bak" "$f"; done
```

Два следствия, которые стоит знать:

- Ядро ничего не кеширует между запусками, поэтому замена вступает в силу при следующей записи конфигурации. После этого повторно отправьте `PUT /api/setting` из шага 6.
- Заменённый `geosite.dat` безопасен: российский файл — надмножество `domain-list-community`, поэтому встроенный режим whitelist продолжает работать. Заменённый `geoip.dat` — **не** прямая замена для всех случаев: в нём нет категорий стран, на которые ссылаются встроенные режимы (`cn`, `private`, `hk`, `mo`). Поэтому оба файла должны быть из одного источника.

## Шаг 7 — импорт опубликованного набора правил

```bash
RULES='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-custom-routing-list/main/v2rayN/template.json'

python3 -c "import json;print(json.dumps({'url':'$RULES','remarks':'Заблокированное'}))" > /tmp/imp.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json \
  | python3 -m json.tool
```

**Вы должны увидеть** `alreadyUpToDate: false` и `version`.

Набор называется «Заблокированное», потому что он самый показательный из трёх: в нём вместе используются `proxy`, `block` и `direct`. В двух других почти всё проваливается в значение по умолчанию, поэтому неправильную конфигурацию гораздо легче пропустить.

Подтвердите, что правила прибыли, и проверьте, что теги, которые они называют, действительно есть в файлах на этой машине:

```bash
python3 - "$CFG" <<'PY'
import json,sys
rules = json.load(open(sys.argv[1])).get("routing", {}).get("rules", [])
mine = [r for r in rules
        if any(t.startswith("rule-") for t in (r.get("inboundTag") or []))]
print("rules on rule ports:", len(mine))
PY

ASSETS="${XDG_DATA_HOME:-$HOME/.local/share}/v2raya"
python3 - "$ASSETS/geosite.dat" "$ASSETS/geoip.dat" <<'PY'
import sys
for path, tags in ((sys.argv[1], ("ru-blocked","category-ads-all","private","cn")),
                   (sys.argv[2], ("ru-blocked","private","cn","hk","mo"))):
    raw = open(path,'rb').read().decode('latin-1')
    print(path.rsplit('/',1)[-1], {t: (t.upper() in raw) for t in tags})
PY
```

**Вы должны увидеть** `rules on rule ports: 9` и ни одного `False`. `False` здесь — это то, для предотвращения чего существует шаг 6b.

## Шаг 8 — проверьте каждую ветку

Два значения из шага 5: **A** — прямая точка выхода, **B** — узла.

```bash
P="-x http://127.0.0.1:20172 --max-time 10"

# geoip:private -> direct: should answer fast and give A
curl -s $P http://192.168.0.1/ -o /dev/null -w 'private: %{http_code}\n'
curl -s $P https://ifconfig.me; echo

# geosite:category-ads-all -> block: the connection should fail fast
curl -s $P https://ads.example/ -o /dev/null -w 'ads: %{http_code}\n'

# geosite:ru-blocked -> proxy: should give B
curl -s $P https://<a-domain-from-the-list>/ -o /dev/null -w 'blocked: %{http_code}\n'
curl -s $P https://ifconfig.me; echo
```

| Что вы видите | Что это значит |
| --- | --- |
| `private: 000` через 1–2 секунды | правило сработало, трафик пошёл напрямую |
| `ads: 000` через 1–2 секунды | блокировка работает |
| `blocked: 200` и IP — **B** | заблокированные домены идут через узел |
| `000` после полных 10 секунд | правило **не** сработало; трафик пошёл обычным путём |
| IP — **A** там, где ожидалась **B** | в скачанном файле нет такого тега |

Чтобы выбрать домен для последней строки, прочитайте теги файла, который скачала эта машина:

```bash
ASSET=$(find "$HOME/.config" /var/lib /usr/local/share -name geosite.dat 2>/dev/null | head -1)
python3 -c "
raw = open('$ASSET','rb').read().decode('latin-1')
for t in ('ru-blocked','category-ads-all','private','cn'):
    print(f'{t:20}', 'present' if t.upper() in raw else 'ABSENT')"
```

## Шаг 9 — то же самое из интерфейса

Всё выше доступно без терминала. Откройте **http://127.0.0.1:2017**, войдите с учётной записью из шага 3, затем:

- **Settings → Advanced Settings → RoutingA** — панель источника правил: URL, версия, которую он отдал последней, и выбор, какой набор импортировать. Импорт заменяет правила, поэтому редактор перезагружается и показывает то, что принёс источник.
- **Settings → DNS Settings** — та же панель для DNS-правил.

## Шаг 10 — DNS-правила из источника (необязательно)

DNS-источник публикует правила в собственном формате этого проекта, по одному апстриму и одному исходящему подключению на каждое. Это **не** формат обмена v2rayN, по причине, изложенной в разделе 7.1 [testing-on-a-real-machine.md](testing-on-a-real-machine.md).

```bash
cat > /tmp/dns.json <<'EOF'
{
  "version": "my-dns-1",
  "remarks": "my set",
  "rules": [
    { "upstream": "127.0.0.1", "domain": "geosite:private", "outbound": "direct" },
    { "upstream": "1.0.0.1", "outbound": "proxy" }
  ]
}
EOF

python3 -m http.server 8899 --directory /tmp > /dev/null 2>&1 &
sleep 1

python3 -c 'import json;print(json.dumps({"url":"http://127.0.0.1:8899/dns.json"}))' > /tmp/dnssrc.json
curl -s -X PUT http://127.0.0.1:2017/api/dnsSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/dnssrc.json \
  | python3 -m json.tool
```

**Вы должны увидеть** `alreadyUpToDate: false`. Правила попадают в сгенерированную конфигурацию под `dns_module`, а не под `routing`. Отвечает ли затем модуль — отдельный вопрос — см. открытый пункт в конце; проверьте с помощью `dig @127.0.0.1 -p 52353 example.com`, прежде чем доверять этому:

```bash
python3 - "$CFG" <<'PY'
import json,sys
dns = json.load(open(sys.argv[1])).get("dns_module") or {}
print("module keys:", sorted(dns.keys()))
for r in dns.get("rules", []):
    print("  ", r.get("action","route"), r.get("outbound","direct"), "<-", r.get("domain","*"))
PY
```

## Когда что-то пошло не так

| Симптом | Причина | Что делать |
| --- | --- | --- |
| 2017 не отвечает при первом запуске | geo-файлы скачиваются, ~90 МБ | подождите две минуты, затем `tail /tmp/v2raya.log` |
| Шаг 5: A равно B | узел не несёт трафик | проверьте `running: True` из шага 4, затем лог |
| `no server is selected` | вызов подключения из шага 4 пропущен | выполните вызов подключения, затем снова запустите ядро |
| `curl: (7) Failed to connect` на 20172 | rule-порт не в режиме RoutingA | `pacMode` должен быть `routingA`, см. шаг 6 |
| `pacMode` остался старым значением после PUT | тело было обёрнуто в `{"setting": …}` | тело — сам объект настройки, см. шаг 6 |
| `on rule ports: 0` | правила не применились | повторите шаг 6 и подтвердите `pacMode` |
| `invalid config: the core could not restart` | правило называет тег, которого нет в geo-файлах | шаг 6b; деталь — в логе |
| 401 на любой запрос | токен устарел или изменился | `export TOKEN=$(cat /tmp/token)` |
| `REQUEST_IN_PROGRESS` | выполняется другая мутация | подождите пять секунд и повторите |
| Правила «вроде работают» на 20170 | их там нет вообще | используйте **20172** |
| Импорт отвечает `ROUTING_RULES_INCOMPATIBLE` | набор совпадает по имени процесса, чего RoutingA не может выразить | выберите другой набор или сообщите о пробеле в апстрим |

Два шага, которые нельзя пропускать, — это **шаг 5** и проверка конфигурации в **шаге 6**. Без них мёртвый узел неотличим от набора правил, который не работает.

## Очистка

```bash
pkill -f 'v2raya --lite'
rm -rf "$HOME/.config/v2raya"     # this removes every setting
```

## Что наблюдалось, а что нет

Шаги 1–10 были выполнены на Linux-хосте 2026-09-27 на сборке этого checkout с рабочим узлом VLESS (xhttp+tls). Прогон нашёл три бага, все исправлены и покрыты тестами; исправления — это то, что несёт этот checkout.

**Прогон по порядку.** Последовательность сборки работает как написано. Тела API — как задокументировано, кроме `PUT /api/setting`, чьё тело — сам объект настройки: обёртка `{"setting": …}` молча игнорируется (обнаружено так, после того как режим, казалось, не вступил в силу). Выбор узла запускает ядро, так что последующий вызов запуска лишь подтверждает. A и B различаются, и оба стабильны при повторных запросах. Контрольное правило `domain(ifconfig.me)->direct` на порту 20172 возвращает прямую точку выхода, а значение по умолчанию — узла, что доказывает механизм до любого импорта.

Импорт опубликованного индекса и набора «Заблокированное» по сети завершается успешно и сообщает `version: RUv1`, с одиннадцатью правилами на rule-портах (девять из набора, правило Apple push и строка по умолчанию от конвертера). `domainStrategy: IPOnDemand` переносится из набора в конфигурацию. Повторный импорт отвечает `alreadyUpToDate: true`.

**По веткам, сверено с логом ядра.** Ядро логирует `taking detour [X]` (выбор маршрута) на подключение, поэтому каждый исход ниже прочитан из лога, а не выведен из таймингов:

| Назначение | Detour | Наблюдалось |
| --- | --- | --- |
| 192.168.0.1:80 | direct (geoip:private) | быстрый отказ |
| 4pda.to:443, участник geosite:ru-blocked в скачанном файле | proxy | ответ через узел (HTTP 403 от сайта) |
| рекламный домен из geosite:category-ads-all | block | соединение убито за ~0,01 с |
| neverssl.com:80 | direct (замыкающее правило) | быстрый отказ |

Порт SOCKS5-with-rules (20173, включённый для проверки) маршрутизирует через тот же набор: несовпавший домен возвращает прямую точку выхода, как говорит замыкающее правило. Сама загрузка источника правил была видна в логе ядра идущей через локальное SOCKS входящее подключение в исходящее подключение прокси (`accepted tcp:raw.githubusercontent.com:443 [socks_ipv4 -> proxy]`), что доказывает путь скачивания через прокси.

**Интерфейс управлялся вживую, а не мокировался.** Playwright (Chromium, headless) против живого бэкенда: вход, закрытие обучающего руководства при первом запуске, открытие Settings, открытие диалога RoutingA. Панель показывает URL, выбор набора на «Заблокированное», `Version: RUv1` и стратегию доменов; текстовый редактор содержит импортированные правила (`# RUv1-Заблокированное`, `protocol('bittorrent')`, `geosite:ru-blocked`). Диалог DNS показывает свою панель. Открытие узла со страницы Proxies через Modify и сохранение без изменений сохраняет параметр `encryption` на share-ссылке, и узел продолжает работать — это прогоняет оба кодека end-to-end. Спеки лежат рядом с тестовой сессией, а не в репозитории; компонентные тесты покрывают те же компоненты на моках.

**Четыре бага, найденные этим прогоном.** Все четыре были бисецированы вживую, исправлены и покрыты юнит-тестами плюс живым повторным прогоном:

1. **Ссылки VLESS с `encryption` были молча сломаны.** Парсер игнорировал параметр и писал `"none"`, поэтому туннель устанавливался, а сервер бесшумно ронял каждый стрим. Исправлено в обоих парсерах (Go и кодек GUI), заодно принимаются `mode`, `x_padding_bytes` и объект `extra`. Без этого исправления ничто ниже шага 5 не может работать для таких ссылок.
2. **Каждый ненулевой XHTTP-диапазон останавливал ядро.** `XHTTPRangeConfig` сериализовался как `{"from":..,"to":..}`, тогда как загрузчик ядра принимает только `"min-max"` или обычное целое число. Это ломало паддинг из импорта — и сломало бы любой диапазон, заданный через GUI. Теперь структура маршалится как `"100-1000"`.
3. **Сервис без root мог запросить у DNS-модуля порт 53.** Генератор добавляет слушатель `127.2.0.17:53`, когда порт выглядит свободным, но свободный не значит доступный для bind без привилегий, и тогда ядро завершается при старте. Теперь генератор пропускает слушатель без root, а слушатель ядра трактует дополнительные адреса как best-effort: тот, что не удаётся забиндить, пропускается с предупреждением вместо провала всего модуля.
4. **DNS-модуль разрешал «через прокси» напрямую.** Адаптер диспетчера ставил тег прокси как *входящий* тег, который не совпадает ни с одним правилом маршрутизации, поэтому запрос проваливался в маршрут по умолчанию — direct. Обычный UDP и DoH-апстримы с `outbound: proxy` оба отвечали SERVFAIL (прямой UDP отфильтрован в той песочнице, прямой TCP/53 отравлен), тогда как curl через SOCKS входящее подключение работал. Теперь адаптер форсирует исходящее подключение напрямую в контексте диспетча (`SetForcedOutboundTagToContext`, тот же механизм, которым xray набирает тегированное исходящее подключение), в обход правил. Теги групп-балансировщиков не имеют обработчика исходящих подключений для форсирования, поэтому генератор отдаёт их как `group_tags`, а модуль отправляет эти апстримы через локальное SOCKS входящее подключение, где применяется обычная маршрутизация — включая балансировщики. SOCKS-путь также выучил `https` (DoH POST поверх TLS) и `tls`, которые раньше он пытался как обычный DNS-over-TCP.

Что этот прогон **не** установил:

- прозрачные режимы и Port Sharing, которые меняют хост;
- DNS-источник против настоящего удалённого URL; рецепт отдаёт локальный файл;
- срабатывание ежедневных тиков источников; покрыт только кодовый путь за ними.

### Группы прокси, проверенные вживую с двумя узлами

Более поздний прогон 2026-09-27 импортировал второй узел VLESS (тот же хост, другой gateway) и переместил оба в группу прокси `eu` (leastping, по умолчанию). Что установил этот прогон, бэкенд и интерфейс:

- генератор отдаёт `group_tags: ["eu", "proxy"]` — обе группы держат обоих участников, поэтому обе являются балансировщиками — с исходящими подключениями участников, тегированными на узел;
- DNS-правило с `outbound: eu` отвечает через группу: лог ядра показывает запрос на локальном SOCKS входящем подключении в выбор балансировщика (`accepted tcp:1.0.0.1:53 [socks_ipv4 -> <EU member>]`), никогда не форсированный detour — путь группы, а не одиночного исходящего подключения;
- трафик с `default: eu` идёт (12/12 HTTP 200) и каждый раз попадает на более быстрого участника, как и должен leastping;
- `PUT /api/outboundSelection` прикалывает группу к узлу 1 (`taking detour [eu]`, всё ещё 200), а `which: null` возвращает её к балансировке (тег участника снова в логе);
- Playwright, та же сессия: страница Proxies показывает обе карточки узлов; меню групп дашборда открывает настройки EU (probe URL, 60 с, Least Latency First); диалог DNS предлагает `eu` рядом с `direct`, и переключение catch-all на `eu` и обратно через GUI прилипает на бэкенде в обе стороны; сохранение второго узла без изменений сохраняет его параметр `encryption`. Всего семь спеков зелёные.

### Закрыто: DNS-модуль отвечает через прокси

Ранее открытый пункт нашёл первопричину и исправлен (баг 4 выше). Проверено вживую 2026-09-27 с узлом VLESS: с правилами по умолчанию `dig @127.0.0.1 -p 52353 example.com` проваливается на апстрим `1.0.0.1` через прокси и отвечает неотравленными адресами, а лог ядра показывает `taking platform initialized detour [proxy] for [tcp:1.0.0.1:53]`. С DoH-правилом (`https://1.0.0.1/dns-query`, `outbound: proxy`) тот же запрос тоже отвечает, через `taking platform initialized detour [proxy] for [tcp:1.0.0.1:443]`. UDP-точка выхода песочницы никогда не была проблемой — модуль просто никогда не набирал через прокси.
