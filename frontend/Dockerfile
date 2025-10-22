FROM oven/bun:1-alpine AS builder
WORKDIR /app
COPY package.json .
RUN bun install
COPY . .
RUN bun run build

FROM nginx:1.29.2-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]

