# napi-avahi

Найти NAPI в сети, не зная её IP.

Пакет даёт плате mDNS-имя `napi<SoC>-<4 последних знака MAC>.local`, например
`napi3308-b9df.local` (NAPI-C, RK3308) или `napi3568-1c2e.local` (NAPI2, RK3568),
и объявляет службу `_napi._tcp` с полями TXT:

| поле | пример |
|------|--------|
| `model` | `NAPI-C` |
| `soc` | `rk3308` |
| `os` | `armbian-26.11.0-trunk.117`, `napilinux-1.4` |
| `role` | `donoff,mgate` (задаёте вы) |
| `mac` | `d2:a9:fd:81:b9:df` |
| `kernel` | `6.18.62-current-rockchip64` |

Имя и поля обновляются при каждой загрузке, до запуска avahi. Системный hostname не меняется.

## Установка на NAPI

```sh
curl -fsSL https://repo.napilab.ru/napilab.gpg | gpg --dearmor > /usr/share/keyrings/napilab.gpg
echo "deb [arch=arm64 signed-by=/usr/share/keyrings/napilab.gpg] https://repo.napilab.ru stable main" > /etc/apt/sources.list.d/napilab.list
apt update && apt install napi-avahi
```

## Команды на плате

```sh
napi-avahi show                 # что объявляет эта плата
napi-avahi role donoff,mgate    # роль (пусто: без роли)
napi-avahi name kotelnaya       # своё имя вместо napi<SoC>-<MAC> (пусто: вернуть)
napi-avahi list                 # все NAPI в сети
```

Настройки лежат в `/etc/napi/avahi.conf` (`ROLE=`, `NAME=`); после ручной правки `napi-avahi update`.

## Поиск с компьютера

- **Windows, окно:** `napi-finder.exe`. Кнопка «Искать», таблица NAPI; кнопки
  napiconfig (8080), influxdb (8086), grafana (3000), donoff (8090) открывают выбранную плату
  в браузере, двойной щелчок копирует IP. Если Windows спросит про брандмауэр, разрешите
  для частной сети.
- **Windows и Linux, консоль:** `napi-discover` (`-t 5s` слушать дольше).
- **Linux:** `avahi-browse -rt _napi._tcp`.
- Просто по имени: `ping -4 napi3308-b9df.local`, `http://napi3308-b9df.local:8090`.

Утилиты спрашивают mDNS с обычного UDP-порта (legacy unicast), поэтому не мешают
mDNS-службе Windows и работают без установки Bonjour.

## Сборка

```sh
make test        # тесты скрипта на поддельной плате
make deb         # build/napi-avahi_<версия>_all.deb
make tools       # build/napi-finder.exe, napi-discover(.exe), napi-discover-linux-{amd64,arm64}
```

Пакет для repo.napilab.ru собирает `build-napi-avahi.sh` из
[lab240/repo-napilab](https://github.com/lab240/repo-napilab).
