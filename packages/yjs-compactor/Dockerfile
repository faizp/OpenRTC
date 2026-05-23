FROM node:22-alpine

WORKDIR /app

RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY packages packages

RUN pnpm install --frozen-lockfile

CMD ["pnpm", "--filter", "@openrtc/yjs-compactor", "compact"]
