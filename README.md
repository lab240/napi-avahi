# napi-avahi

Найти NAPI в сети, не зная её IP.

Пакет даёт плате mDNS-имя `napi<SoC>-<id>.local`, например `napi3308-79df.local`
(NAPI-C, RK3308) или `napi3568-1c2e.local` (NAPI2, RK3568), и объявляет службу `_napi._tcp`
с полями TXT.

`id`: 4 знака md5 от серийного номера чипа (`/proc/device-tree/serial-number`). MAC для этого
не годится: Armbian выводит его из части серийника, и у разных плат он бывает одинаковым (как и
конец серийника). Если серийника нет, берётся конец MAC.

Поля TXT:

| поле | пример |
|------|--------|
| `model` | `NAPI-C` |
| `soc` | `rk3308` |
| `os` | `armbian-26.11.0-trunk.117`, `napilinux-1.4` |
| `role` | `donoff,mgate` (задаёте вы) |
| `mac` | `d2:a9:fd:81:b9:df` |
| `serial` | `9e098bbcae8b6599` |
| `kernel` | `6.18.62-current-rockchip64` |

Имя и поля обновляются при каждой загрузке, до запуска avahi. Системный hostname не меняется.

## Установка на NAPI

```sh
curl -fsSL https://repo.napilab.ru/napilab.gpg | gpg --dearmor > /usr/share/keyrings/napilab.gpg
echo "deb [arch=arm64 signed-by=/usr/share/keyrings/napilab.gpg] https://repo.napilab.ru stable main" > /etc/apt/sources.list.d/napilab.list
apt update && apt install napi-avahi
```

## NapiLinux (Yocto)

Рецепт `yocto/napi-avahi_git.bb`: положить в слой, добавить в образ
`IMAGE_INSTALL:append = " napi-avahi"`. Тянет `avahi-daemon` и `avahi-utils`, ставит
скрипт, включает `napi-avahi.service`, `/etc/napi/avahi.conf` помечен как `CONFFILES`.
Скрипт проверен на busybox. Перед релизом закрепить `SRCREV` и указать лицензию.

## Команды на плате

```sh
napi-avahi show                 # что объявляет эта плата
napi-avahi role donoff,mgate    # роль (пусто: без роли)
napi-avahi name kotelnaya       # своё имя вместо napi<SoC>-<id> (пусто: вернуть)
napi-avahi list                 # все NAPI в сети
```

Настройки лежат в `/etc/napi/avahi.conf` (`ROLE=`, `NAME=`); после ручной правки `napi-avahi update`.

## Поиск с компьютера

- **Windows:** один файл `napi-finder.exe`. Двойной щелчок: окно с кнопкой «Искать» и таблицей NAPI; кнопки
  napiconfig (8080), influxdb (8086), grafana (3000), donoff (8090) открывают выбранную плату
  в браузере, двойной щелчок копирует IP. Если Windows спросит про брандмауэр, разрешите
  для частной сети. «Журнал» показывает, что делал последний поиск.
  В консоли: `.\napi-finder.exe --console` (`-v` журнал, `-t 5s` слушать дольше).
- **Linux, консоль:** `napi-discover` (`-v`, `-t 5s`).
- **Linux:** `avahi-browse -rt _napi._tcp`.
- Просто по имени: `ping -4 napi3308-79df.local`, `http://napi3308-79df.local:8090`.

Утилиты спрашивают mDNS с обычного UDP-порта (legacy unicast), поэтому не мешают
mDNS-службе Windows и работают без установки Bonjour. Кроме общего запроса (multicast) они
спрашивают напрямую каждый адрес своей подсети: многие точки доступа Wi-Fi не пропускают
multicast из проводной сети к своим клиентам, и NAPI на Wi-Fi общий запрос не слышит.

## Сборка

```sh
make test        # тесты скрипта на поддельной плате
make deb         # build/napi-avahi_<версия>_all.deb
make tools       # build/napi-finder.exe, napi-discover-linux-{amd64,arm64}
```

Пакет для repo.napilab.ru собирает `build-napi-avahi.sh` из
[lab240/repo-napilab](https://github.com/lab240/repo-napilab).
