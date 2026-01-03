/**
 * Basic Metro configuration for the TurboDriver RN sandbox.
 */
const {getDefaultConfig, mergeConfig} = require('metro-config');

const customConfig = {
  resolver: {
    extraNodeModules: {
      // Work around RN asset registry resolution for LogBox images
      'missing-asset-registry-path': require.resolve(
        'react-native/Libraries/Image/AssetRegistry',
      ),
    },
    assetRegistryPath: require.resolve(
      'react-native/Libraries/Image/AssetRegistry',
    ),
  },
};

module.exports = (async () => {
  const defaults = await getDefaultConfig(__dirname);
  return mergeConfig(defaults, customConfig);
})();
