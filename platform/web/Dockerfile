# syntax=docker/dockerfile:1
FROM docker.1ms.run/library/node:24-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM docker.1ms.run/library/nginx:1.29-alpine
COPY web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
