FROM node:20-alpine

WORKDIR /docs

# Install mintlify CLI globally
RUN npm install -g mintlify

# Expose the default port
EXPOSE 3000

# Run mintlify dev server
CMD ["mintlify", "dev", "--port", "3000", "--host", "0.0.0.0"]
