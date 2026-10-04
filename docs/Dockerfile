FROM node:24-alpine

WORKDIR /docs

RUN npm install -g mint@4.2.984

EXPOSE 3000

CMD ["mint", "dev", "--port", "3000", "--no-open"]
