FROM node:22-alpine AS build
WORKDIR /app
ARG VITE_OIDC_ISSUER
ARG VITE_OIDC_CLIENT_ID=aos-spa
ARG VITE_AOS_URL
ARG VITE_PRUDENCE_URL
ARG VITE_ERNEST_URL
ENV VITE_OIDC_ISSUER=$VITE_OIDC_ISSUER
ENV VITE_OIDC_CLIENT_ID=$VITE_OIDC_CLIENT_ID
ENV VITE_AOS_URL=$VITE_AOS_URL
ENV VITE_PRUDENCE_URL=$VITE_PRUDENCE_URL
ENV VITE_ERNEST_URL=$VITE_ERNEST_URL
COPY package*.json ./
# @chestertown/design comes from the vendored tarball pinned in package.json.
COPY vendor ./vendor
RUN npm ci
COPY . .
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
