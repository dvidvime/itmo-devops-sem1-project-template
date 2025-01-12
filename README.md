# Финальный проект 1 семестра

REST API сервис для загрузки и выгрузки данных о ценах.

## Требования к системе

Сервис написан на go 1.23.3 и использует БД Postgres 17

Работа протестирована на Ubuntu 24.04 и macOS Sequoia, возможен запуск и на других ОС

## Установка и запуск

### Локальная сборка и запуск с помощью Docker Compose

1. `cp .env.example .env` - заменить в файле `.env` настройки соединения с БД Postgres на свои

2. `docker compose up -d`

### Развертывание последней версии из Dockerhub в Yandex Cloud 

Скрипт `./scripts/run.sh` создаст сервер через Yandex Cloud CLI и разворачивает приложение и базу данных через Docker на удалённом сервере через ssh. В результате работы скрипт выведет IP созданного сервера и ssh-ключ для подключения к нему.

#### Необходимые переменные окружения:
* **YC_TOKEN**=*< [токен авторизации yandex cloud](https://yandex.cloud/ru/docs/cli/quickstart) >*
* **YC_CLOUD_ID**=*< идентификатор [облака](https://yandex.cloud/ru/docs/resource-manager/operations/cloud/create) в yandex cloud >*

#### Дополнительные параметры:
* **NAME_PREFIX**=*< префикс для создаваемых ресурсов (по умолчанию: my) >*
* **SSH_PASSPHRASE**=*< фраза для ssh-ключа (по умолчанию отсутствует) >*
* **YC_ZONE**=*< [зона](https://yandex.cloud/ru/docs/overview/concepts/geo-scope) в yandex cloud (по умолчанию: ru-central1-a) >*
* **YC_FOLDER**=*< [каталог](https://yandex.cloud/ru/docs/resource-manager/operations/folder/create) в yandex cloud (по умолчанию: default) >*

## Тестирование

Директория `sample_data` - это пример директории, которая является разархивированной версией файла `sample_data.zip`

### Пример загрузки данных
`curl -X POST -F "file=@sample_data.zip" http://localhost:8080/api/v0/prices`

### Автоматическое тестирование 

С помощью скрипта `./scripts/tests.sh  <уровень_проверки>`

Уровень проверки должен быть:
* 1 - простой уровень
* 2 - продвинутый уровень
* 3 - сложный уровень

## Контакт

Telegram [@dedushkinw](https://t.me/dedushkinw)