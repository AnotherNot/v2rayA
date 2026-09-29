export const template = `default: proxy
# write your own rules below
domain(geosite:private)->direct
ip(geoip:private)->direct`;
