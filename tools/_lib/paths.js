// Central path helper: all tools should resolve runtime data through this.
const path = require('path');

const dataDir = process.env.OPENCODE_FARM_DATA_DIR ||
  path.resolve(__dirname, '..', '..', '..', 'opencode-farm-data');

const paths = {
  dataDir,
  gatewayConfig: path.join(dataDir, 'gateway', 'config.json'),
  proxyTokens: path.join(dataDir, 'proxy', '.tokens.txt'),
  clientCredentials: path.join(dataDir, 'client', '.credentials.yaml'),
  clientSettings: path.join(dataDir, 'client', 'settings.yaml'),
};

module.exports = paths;
