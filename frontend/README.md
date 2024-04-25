# Market UI ![Docker Image Version (tag)](https://img.shields.io/docker/v/beclab/market-frontend/v0.1.53) ![Docker Image Size (tag)](https://img.shields.io/docker/image-size/beclab/market-frontend/v0.1.53)

## Configuring

Create an .env file with the following

```
ACCOUNT=terminusName
DEV_DOMAIN=www.terminusName.myterminus.com

```

Edit hosts with The DEV_DOMAIN environment variable value configured above

```
127.0.0.1 www.terminusName.myterminus.com
```

## Install the dependencies

```bash
npm install
```

### Start the app in development mode (hot-code reloading, error reporting, etc.)

```bash
npm run dev
```

### Build the app for production

```bash
npm run build
```

### Customize the configuration

See [Configuring quasar.config.js](https://v2.quasar.dev/quasar-cli-webpack/quasar-config-js).
