# syntax=docker/dockerfile:1
# crawler web (admin spa): sveltekit 3 + adapter-node. runtime: numeric non-root uid, read-only rootfs ok.

ARG NODE_IMAGE=node:26-alpine

FROM ${NODE_IMAGE} AS base
# node 26 images ship without yarn/corepack. pin yarn classic.
RUN npm install -g yarn@1.22.22 && npm cache clean --force
WORKDIR /app

FROM base AS deps
COPY package.json yarn.lock .npmrc ./
RUN yarn install --frozen-lockfile --non-interactive --ignore-scripts

FROM deps AS build
COPY . .
# optional: bake origin for kit csrf/url.origin (paths.origin). runtime ORIGIN works too.
ARG PUBLIC_ORIGIN=
ENV PUBLIC_ORIGIN=${PUBLIC_ORIGIN}
RUN yarn svelte-kit sync && yarn build

FROM base AS prod-deps
COPY package.json yarn.lock .npmrc ./
RUN yarn install --frozen-lockfile --production --non-interactive --ignore-scripts && yarn cache clean

FROM ${NODE_IMAGE} AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOST=0.0.0.0 \
    API_INTERNAL_URL=http://api:8080 \
    PUBLIC_ORIGIN=http://localhost:3000 \
    ORIGIN=http://localhost:3000 \
    BODY_SIZE_LIMIT=1M \
    ADDRESS_HEADER=X-Forwarded-For \
    XFF_DEPTH=1
# ORIGIN = public origin (kit csrf check on proxied POSTs). must match backend CRAWLER_PUBLIC_URL.
# behind a proxy: client ip = last XFF hop. ssr forwards it to api.
# no proxy in front -> override ADDRESS_HEADER= (empty) to use socket address.
# root-owned, read-only for the app user. nothing written at runtime.
COPY --from=prod-deps /app/node_modules ./node_modules
COPY --from=build /app/build ./build
COPY package.json ./
USER 1000:1000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:'+(process.env.PORT||3000)+'/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "build"]
