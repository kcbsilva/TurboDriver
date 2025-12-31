/**
 * Basic Metro configuration for the TurboDriver RN sandbox.
 */
const {getDefaultConfig, mergeConfig} = require('@react-native/metro-config');

const config = {};

module.exports = mergeConfig(getDefaultConfig(__dirname), config);
