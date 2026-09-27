# Тестирование v2rayA на реальной машине

Процедура проверки на живой машине того, что автоматизированные тесты могут лишь
приблизительно оценить: что узел подключается, что набор правил доходит до ядра и что
трафик действительно идёт по ветви, которую называют правила.

Части этого документа были прогнаны на сборке этого чекаута; в последнем
разделе сказано, какие части, а какие — нет. Каждая команда выведена из кода
и из тестов уровня сервиса в
`service/server/service/routingsource_smoke_test.go` и `dnssource_test.go`,
которые отрабатывают полный путь загрузка-конвертация-сохранение на опубликованных
документах, отдаваемых локально. Обновляйте этот документ при изменении набора функций
и фиксируйте фактически наблюдённое в последнем разделе.

Более короткий путь, и тот, с которого следует начать, см. в
[running-from-source.md](running-from-source.md): сборка, запуск и доказательство того, что узел
подключается. Этот документ — длинная версия: что доказывает каждая проверка и как
читать неверный результат.

## Перед началом

### Режим по умолчанию не затрагивает машину

С `transparent: close` — значением по умолчанию (`service/db/configure/setting.go`) —
v2rayA не устанавливает правил firewall, не трогает `/etc/resolv.conf` и не открывает
TUN-устройства. Поэтому вся процедура ниже выполняется **без root и
без единого изменения сети машины**.

Это не тестовый режим; это обычный режим «явного прокси». Приложение
указывает на SOCKS, а v2rayA несёт трафик в VLESS.

### Какие порты несут правила

| Порт | Тег ядра | Получает правила RoutingA |
| --- | --- | --- |
| 20170 | `socks` | нет |
| 20171 | `http` | нет |
| 20172 | `rule-http` | да |
| `socks5WithPac` | `rule-socks` | да |

Правила прикреплены только к `rule-http` и `rule-socks`
(`service/kernel/v2ray/template_routing.go`, `setRulePortRouting`). Клиент,
направленный на 20170 или 20171, не увидит импортированный набор правил. Это наиболее
вероятная причина «правила ничего не делают».

`socks5WithPac` по умолчанию равен `0`, то есть входящее подключение SOCKS5-с-правилами выключено,
пока вы его не включите.

### Сначала докажите механизм, потом доверяйте импорту

Выполняйте их по порядку. Отлаживать сбой подключения и сбой конвертации сразу
гораздо сложнее, чем доказать каждое по отдельности.

1. Покажите, что узел работает: прямой запрос и запрос через порт 20170 возвращают
   разные выходные адреса.
2. Напишите одно правило, исход которого вам уже известен, и проверьте его.
3. Только затем импортируйте опубликованный набор правил.

## 1. Сборка и запуск

### 1.1 Сборка

```sh
docker run --rm -v "$PWD:/repo" -w /repo/gui node:24 \
  bash -c 'corepack enable >/dev/null && yarn install --frozen-lockfile && yarn build'
mkdir -p service/server/router/web && cp -a web/. service/server/router/web/
docker run --rm -v "$PWD:/repo" -w /repo/core golang:1.26 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya_core ./main'
docker run --rm -v "$PWD:/repo" -w /repo/service golang:1.26 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya .'
```

`./build.sh` делает то же самое с локальными Go 1.26 и yarn.

### 1.2 Запуск без затрагивания системы

```sh
./v2raya --lite
```

Без root конфигурация живёт в `$XDG_CONFIG_HOME/v2raya`
(`service/conf/environmentConfig_other.go`); сгенерированная конфигурация ядра —
`<config dir>/config.json`.

```sh
curl -s http://127.0.0.1:2017/api/version | head -c 400
ss -lntp | grep -E '2017|20170|20171|20172'
```

### 1.3 Первый запуск: учётная запись

API отвечает `first: true`, пока учётная запись не существует.

```sh
curl -s -X POST http://127.0.0.1:2017/api/account \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"CHANGE_ME"}'

TOKEN=$(curl -s -X POST http://127.0.0.1:2017/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"CHANGE_ME"}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
```

Каждый последующий запрос требует `Authorization: Bearer $TOKEN`.

## 2. Узел и контрольное измерение

Добавьте узел, выберите его и запустите ядро. Тело запроса импорта называет ссылку под
`url`, в единственном числе, а выбор узла — отдельный вызов, который должен произойти
до запуска ядра: `StartV2ray` отказывает с `no server is selected`.

```sh
curl -s -X POST http://127.0.0.1:2017/api/import \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"vless://<uuid>@host:443?security=tls&sni=example.com&type=ws#my-node"}'

SERVER_ID=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["touch"]["servers"][0]["id"])')

curl -s -X POST http://127.0.0.1:2017/api/connection \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"_type\":\"server\",\"id\":$SERVER_ID,\"sub\":0,\"outbound\":\"proxy\"}"

# this request takes no body
curl -s -X POST http://127.0.0.1:2017/api/v2ray -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys; print("running:", json.load(sys.stdin)["data"]["running"])'
```

Вся последовательность — в [running-from-source.md](running-from-source.md), шаг 4.

Затем измерьте два адреса, с которыми сравнивается всё остальное:

```sh
curl -s https://ifconfig.me; echo                                   # A: this host
curl -s --socks5-hostname 127.0.0.1:20170 https://ifconfig.me; echo # B: the node
```

`--socks5-hostname` (`socks5h` в curl) передаёт **домен** прокси.
Без него curl резолвит локально и отправляет адрес, а ядро никогда не видит
имени, поэтому правила `geosite:` не могут сработать.

Если A равно B, узел не работает; ничто ниже вам ничего не скажет.

## 3. Доказательство, что правила маршрутизации доходят до ядра

### 3.1 Напишите правило с известным исходом

```sh
cat > /tmp/known.txt <<'EOF'
# control rule: example.com goes out directly, everything else through the proxy
domain(example.com)->direct
default: proxy
EOF

python3 -c 'import json; print(json.dumps({"routingA": open("/tmp/known.txt").read()}))' > /tmp/r.json
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/r.json
```

### 3.2 Включите RoutingA

`pacMode` — это JSON-имя `RulePortMode` (`service/db/configure/setting.go`).
`PUT /api/setting` принимает весь объект, поэтому прочитайте, измените, запишите:

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["pacMode"]="routingA"
print(json.dumps(s))' > /tmp/s.json
curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/s.json
```

### 3.3 Подтвердите, что правила есть в конфигурации ядра

Это самая важная проверка. Если ни одно правило не несёт `inboundTag: rule-*`,
RoutingA не была применена, и каждое измерение ниже бессмысленно.

```sh
CFG="${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/config.json"
python3 - "$CFG" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))
rules=c.get("routing",{}).get("rules",[])
print("rules:",len(rules))
for r in rules:
    tags=r.get("inboundTag") or []
    if any(t.startswith("rule-") for t in tags):
        what=r.get("domain") or r.get("ip") or r.get("port") or r.get("network")
        print(" ",r.get("outboundTag") or r.get("balancerTag"),"<-",what)
PY
```

### 3.4 Измерьте

```sh
P="-x http://127.0.0.1:20172"
curl -s $P https://example.com     -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo    # expect A
curl -s $P https://api.github.com -o /dev/null -w '%{http_code}\n'
```

Правило `-> direct` должно дать A, правило `-> proxy` должно дать B. Если оба дают
B, правило не применилось.

## 4. Импорт опубликованного набора правил

> **Сначала установите geo-файлы, которые называют правила.** v2rayA скачивает
> `geoip.dat` от v2fly и `dlc.dat` от `domain-list-community`, а опубликованных российских
> категорий нет ни в одном из них. Импорт ниже сообщит `SUCCESS` и сохранит
> правила, а **затем ядро откажется запускаться** с
> `illegal ip rule: geoip:ru-blocked > failed to check code RU-BLOCKED from
> geoip.dat > EOF` — детали в логе v2rayA, API сообщает только
> `invalid config: the core could not restart with the new settings, the previous
> ones are back`, а настройка тихо откатывается. `EOF` там означает, что
> код не найден, а не то, что загрузка была обрезана. Шаг 6b
> [running-from-source.md](running-from-source.md) содержит команды, а проверка
> тегов из 4.6 ниже — это то, что нужно выполнить до включения RoutingA.

### 4.1 Источник

```sh
RULES_URL='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-custom-routing-list/main/v2rayN/template.json'
```

### 4.2 Сначала прочитайте индекс

```sh
python3 -c "import json;print(json.dumps({'url':'$RULES_URL'}))" > /tmp/src.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/src.json
```

`GET /api/routingSource` возвращает `items[]` — имена за источником.
Опубликованный индекс предлагает «Всё», «Всё, кроме РФ» и «Заблокированное».

Начните с **«Заблокированное»**. Это самый показательный набор, потому что он использует
`proxy`, `block` и `direct` вместе, и потому что всё остальное проваливается
в `default: proxy` — в двух других наборах неправильную конфигурацию гораздо
легче пропустить.

### 4.3 Импорт

```sh
python3 -c "import json;print(json.dumps({'url':'$RULES_URL','remarks':'Заблокированное'}))" > /tmp/imp.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json
```

Ожидайте `{"alreadyUpToDate": false, "version": "RUv1", "remarks": "Заблокированное"}`.

### 4.4 Что должно быть в конфигурации ядра

Повторите 3.3. Девять правил, в таком порядке:

```
block      <- domain(geosite:category-ads-all)
direct     <- protocol(bittorrent)
direct     <- ip(geoip:private)
direct     <- domain(geosite:private)
proxy      <- ip(1.0.0.1, 1.1.1.1, 8.8.8.8, 8.8.4.4)
proxy      <- network(udp) + port(50000-65535)
proxy      <- ip(geoip:ru-blocked)
proxy      <- domain(geosite:ru-blocked)
direct     <- port(0-65535)
```

Конвертер называет набор в комментарии (`# RUv1-Заблокированное`); комментарии
не попадают в конфигурацию.

### 4.5 Проверьте каждую ветвь

```sh
P="-x http://127.0.0.1:20172"

# geoip:private -> direct. Must return A, and must not stall.
curl -s $P --max-time 10 http://192.168.0.1/ -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo

# geosite:category-ads-all -> block. The connection must fail fast.
curl -s $P --max-time 10 https://ads.example/ -o /dev/null -w '%{http_code}\n'

# geosite:ru-blocked -> proxy. Must return B.
curl -s $P --max-time 10 https://<a-domain-from-the-list>/ -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo

# the trailing port(0-65535) -> direct. Must return A, and must not stall.
curl -s $P --max-time 10 http://neverssl.com -o /dev/null -w '%{http_code}\n'
```

Сработавшее правило выглядит как быстрый отказ или быстрый возврат A. Правило,
которое **не** сработало, обычно выглядит как соединение, которое висит до таймаута,
потому что трафик пошёл собственным путём прокси.

### 4.6 Подтвердите, что теги есть в скачанном файле

Если третий случай из 4.5 возвращает A вместо B, тег, вероятно, отсутствует в
скачанном файле.

```sh
ASSETS=$(find "${XDG_CONFIG_HOME:-$HOME/.config}" /var/lib /usr/local/share -name geosite.dat 2>/dev/null | head -1)
echo "file: $ASSETS"
python3 - "$ASSETS" <<'PY'
import sys
raw=open(sys.argv[1],'rb').read().decode('latin-1')
for t in ('ru-blocked','category-ads-all','private','cn'):
    print(f"{t:20} {'present' if t.upper() in raw else 'ABSENT'}")
PY
```

Файлы `release` из `runetfreedom/russia-v2ray-rules-dat` были проверены
разбором теми же protobuf-типами, которые использует сервис: `geosite.dat`
несёт 1543 записи, а `geoip.dat` — 266, и оба содержат каждый тег, на который ссылаются поставляемые
наборы. Теги хранятся в верхнем регистре; ядро ищет их через
`strings.EqualFold`, поэтому регистр не важен.

Это проверка *файлов*, а не вашей установки. `False` здесь означает,
что ядро не запустится с импортированными правилами; сначала установите подходящие файлы.

## 5. Идемпотентность, обновление, откат

### 5.1 Повторный импорт ничего не меняет

```sh
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json
```

Ожидайте `"alreadyUpToDate": true`. Смысл проверки: правила, которые вы правили
вручную, не перезаписываются, потому что импорт сравнивает сохранённое с тем,
что дал бы источник.

### 5.2 Откат

```sh
curl -s -X DELETE http://127.0.0.1:2017/api/routingSource -H "Authorization: Bearer $TOKEN"
```

Правила намеренно остаются на месте — пользователь, импортировавший набор, не должен
остаться без правил. Чтобы вернуться к встроенному шаблону, очистите правила:

```sh
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"routingA":""}'
```

Пустое значение откатывается к встроенному шаблону
(`service/db/configure/configure.go`, `GetRoutingA`).

### 5.3 Полный сброс

```sh
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/v2raya.db"*   # this is every setting
```

## 6. Загрузка источника через прокси

`proxyModeWhenSubscribe` по умолчанию равен `direct`, поэтому **по умолчанию источник
загружается без прокси**. Когда машина не может достичь источника напрямую,
измените его.

Прокси-клиент используется, только когда ядро запущено **и** серверы
подключены **и** прозрачный прокси выключен **и** режим — `proxy` или
`pac` (`service/common/httpClient/httpClient.go`, `GetHttpClientAutomatically`).

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["proxyModeWhenSubscribe"]="proxy"
print(json.dumps(s))' > /tmp/sp.json
curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/sp.json
```

Следите за логом, чтобы подтвердить, какой путь был использован:

```sh
tail -f "${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/v2raya.log" | grep -iE 'routing rules|Downloading'
```

**Частая ошибка:** импорт, пока ядро ещё не запущено. Клиентом тогда является
`http.DefaultClient`, и загрузка идёт напрямую. Порядок такой: подключите
узел, запустите ядро, установите `proxyModeWhenSubscribe`, затем импортируйте.

## 7. DNS-правила из источника

Источник правил и DNS-источник разделены: источник правил публикует
правила в формате v2rayN, а DNS-источник публикует их в собственном формате
этого проекта. Раздел 7.1 объясняет, почему эти два формата различаются.

### 7.1 Формат DNS-источника

Принимаемый документ — это либо версионированный объект:

```json
{
  "version": "RUdns1",
  "remarks": "Россия",
  "rules": [
    { "upstream": "127.0.0.1", "domain": "geosite:private", "outbound": "direct" },
    { "upstream": "77.88.8.8", "domain": "geosite:cn", "outbound": "direct" },
    { "upstream": "1.0.0.1", "outbound": "proxy" }
  ]
}
```

либо голый массив тех же объектов правил, и тогда версия берётся
из хеша содержимого, так что неизменённый файл по-прежнему распознаётся как
неизменённый.

Правило без апстрима отклоняется: генератор отбрасывает такое правило, иначе
опечатка в опубликованном документе превратилась бы в запросы, на которые ничего не отвечает. `action`
вне `route`, `reject` и `bypass` отклоняется и называется в ошибке,
потому что его принятие сохранило бы правило, которое генератор игнорирует.

**Что здесь намеренно не принимается.** DNS-шаблон v2rayN — это другое:
сырой DNS-объект ядра (`hosts` плюс плоский список `servers`), доступный
через обёртку, где сопоставление по доменам живёт внутри фрагмента конфигурации.
Опубликованный российский шаблон не несёт ни правил по доменам, ни вообще назначения
исходящих подключений — его серверы суть голые строки.
Его импорт означал бы выдумывание того, какой апстрим отвечает на какое имя, а это
единственное, что в DNS-конфигурации никогда нельзя угадывать. Вместо этого используйте документ
в форме выше.

### 7.2 Импорт и проверка

```sh
DNS_URL='https://example.com/dns_rules.json'
python3 -c "import json;print(json.dumps({'url':'$DNS_URL'}))" > /tmp/dnsrc.json
curl -s -X PUT http://127.0.0.1:2017/api/dnsSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/dnsrc.json
```

Подтвердите, что правила дошли до конфигурации ядра. Они живут в `dns_module`,
а не в `routing`: модуль несёт `upstreams`, `rules`,
`default_upstream`, `bootstrap`, `proxy_map` и `cache`.

```sh
python3 - "$CFG" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))
dns=c.get("dns_module") or {}
print("module keys:", sorted(dns.keys()))
for r in dns.get("rules", []):
    print(" ", r.get("action","route"), r.get("outbound","direct"), "<-", r.get("domain","*"))
print("upstreams:", len(dns.get("upstreams", [])), "default:", dns.get("default_upstream"))
PY
```

### 7.3 Идемпотентность и откат

Те же гарантии, что и для маршрутизации: повторный импорт отвечает
`alreadyUpToDate: true`, ежедневный тик (таймер) перечитывает источник, только когда версия
изменилась, а забывание источника сохраняет правила.

```sh
curl -s -X DELETE http://127.0.0.1:2017/api/dnsSource -H "Authorization: Bearer $TOKEN"
```

### 7.4 В интерфейсе

Оба источника управляются из диалога, владеющего правилами: **Settings →
Advanced Settings → RoutingA** для источника правил, **Settings → DNS
Settings** для DNS-источника. Панель показывает URL, версию, которую источник отдал
последней, и — для источника правил, чей индекс называет несколько наборов, — выбор
того, какой набор импортировать. Ненастроенный источник показывает свою форму открытой.

Импорт заменяет правила, поэтому диалог перезагружает их и показывает то, что
принёс источник, а не то, что было открыто раньше.

## 8. За пределами режима по умолчанию

Каждый пункт ниже меняет машину. Не начинайте здесь, пока разделы 1–7 не
зелёные: сломанный набор правил в сочетании с прозрачным режимом отправляет трафик
в никуда, и вы не сможете понять, что из двух виновато.

### 8.1 SOCKS5 с правилами

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/ports \
  | python3 -c '
import json,sys
p=json.load(sys.stdin)["data"]["ports"]
p["socks5WithPac"]=20173
print(json.dumps(p))' > /tmp/ports.json
curl -s -X PUT http://127.0.0.1:2017/api/ports \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/ports.json

curl -s --socks5-hostname 127.0.0.1:20173 https://ifconfig.me; echo
```

### 8.2 Прозрачный режим

`redirect` и `tproxy` устанавливают правила firewall, `tun` открывает TUN-устройство, а
перехват DNS — часть всех них. Предусловия: root, iptables или
nftables, а для `tun` — устройство `/dev/net/tun`. Правила действуют независимо от
режима, поэтому сначала проверьте их через порт для правил.

### 8.3 Совместное использование портов

Включение `portSharing` переносит встроенные входящие подключения на все интерфейсы.
Встроенные входящие подключения SOCKS и HTTP принимают **без пароля**
(`service/kernel/v2ray/templateJson.go`). В сети, которой вы не доверяете, это
открытый прокси на вашем узле; используйте собственное входящее подключение с именем пользователя и паролем.

## 9. Справочник API

| Метод | Путь | Тело запроса | Примечания |
| --- | --- | --- | --- |
| GET | `/api/routingSource` | — | `{url, version, remarks, domainStrategy, items[]}`; индекс загружается вживую, и его отсутствие — не ошибка |
| PUT | `/api/routingSource` | `{url, remarks?}` | импортирует, перезапускает ядро; пустое `remarks` берёт первый набор |
| DELETE | `/api/routingSource` | — | забывает источник, сохраняет правила |
| GET | `/api/dnsSource` | — | `{url, version, remarks}` |
| PUT | `/api/dnsSource` | `{url}` | импортирует DNS-правила, перезапускает ядро |
| DELETE | `/api/dnsSource` | — | забывает источник, сохраняет правила |
| GET | `/api/setting` | — | `pacMode` выбирает `routingA`; `proxyModeWhenSubscribe` выбирает, как загружаются источники |
| GET | `/api/touch` | — | текущие узлы и подключения |

## 10. Автоматическое покрытие

```sh
cd service
go test -race ./server/service -run 'TestConvert|TestImportRoutingSource|TestSyncRoutingSource|TestImportDnsSource|TestSyncDnsSource'
```

Тесты сервиса конвертируют все три опубликованных набора и разбирают результат через
`RoutingA.Parse`, затем прогоняют весь путь загрузка-конвертация-сохранение на локальном
сервере, отдающем те же файлы. Они не запускают ядро, не открывают сокет и
не трогают сеть машины, поэтому не могут заменить эту процедуру.

Со стороны интерфейса:

```sh
cd gui
yarn test src/dialogs/settings/RuleSource.spec.ts src/dialogs/settings/Dns.spec.ts
```

## 11. Что не наблюдалось

Полный прогон на сборке этого чекаута с рабочим узлом записан
в разделе «Что наблюдалось»
[running-from-source.md](running-from-source.md): A-против-B, доказательство механизма,
импорт, все четыре ветви с логом ядра как оракулом, путь загрузки
через прокси и интерфейс, управляемый Playwright. Тот прогон также нашёл три
бага (потерянный `encryption` у VLESS, объекты-диапазоны, которые ядро отвергает, слушатель
порта 53 без root), все исправлены и покрыты. Ниже — то, что остаётся ненаблюдённым.

- **Разделы 8.2 и 8.3 меняют машину** и не были опробованы в этой
  конфигурации. Правила firewall, файл резолвера и TUN — это части
  v2rayA, которые с наибольшей вероятностью поведут себя иначе, чем следует из чтения кода.
- **DNS-правило с исходящим подключением группы-балансировщика** с тех пор было опробовано на живой
  группе из двух узлов: запрос идёт через локальное входящее подключение SOCKS в
  выбор балансировщика, а GUI прогоняет тег группы в обе стороны; см.
  раздел о группах в [running-from-source.md](running-from-source.md).
  Обычные и DoH-апстримы через прокси также были проверены вживую.
- **Опубликованного набора DNS-правил в форме, описанной в разделе 7.1, не существует.** Тесты
  используют документы, написанные для них. Путь импорта доказан, но ничего
  не импортировалось из настоящего удалённого DNS-источника.
- **Интерфейс управлялся Playwright, а не открывался человеком.** Три
  спека (панель маршрутизации, панель DNS, круговой проход сохранения редактора) проверяют контракт;
  визуальный проход по обоим диалогам ещё предстоит.
- **Ежедневные тики (таймеры) источников** (`conf.TickerUpdateRoutingSource`,
  `conf.TickerUpdateDnsSource`) не наблюдались в срабатывании; покрыт только путь кода
  за ними.
