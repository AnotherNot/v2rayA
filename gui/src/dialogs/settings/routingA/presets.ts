import { template } from "./template";
import { russia } from "./russia";

// Rule sets for the Templates menu: one that starts with `default:` replaces
// the rules, the others are inserted. Outbounds are only proxy, direct and
// block, so a template never fails the save-time check for a group with a
// connected node. Category names must exist in the v2fly geosite.dat and
// geoip.dat that v2rayA downloads: no Loyalsoldier extras such as gfw,
// apple-cn or geoip:telegram. Telegram's ranges are the ones
// template_routing.go uses for the same reason.
const telegramRanges =
  'ip("91.105.192.0/23", "91.108.4.0/22", "91.108.8.0/21", "91.108.16.0/21", "91.108.56.0/22", "95.161.64.0/20", "149.154.160.0/20", "185.76.151.0/24", "2001:67c:4e8::/48", "2001:b28:f23c::/47", "2001:b28:f23f::/48", "2a0a:f280:203::/48")->proxy';

export const presets: { key: string; code: string; region?: boolean }[] = [
  { key: "starter", code: template },
  {
    key: "blacklist",
    code: `default: direct
# only sites outside China and Telegram use the proxy
domain(geosite:geolocation-!cn)->proxy
domain(geosite:telegram)->proxy
${telegramRanges}`,
  },
  {
    key: "global",
    code: `default: proxy
# everything through the proxy except the LAN
domain(geosite:private)->direct
ip(geoip:private)->direct`,
  },
  {
    key: "minimal",
    code: `default: direct
# only the services that need it use the proxy
domain(geosite:google, geosite:youtube, geosite:github, geosite:telegram)->proxy
domain(geosite:category-social-media-!cn, geosite:category-ai-!cn)->proxy
${telegramRanges}`,
  },
  { key: "russia", code: russia },
  {
    key: "ruBlocked",
    region: true,
    code: `# media blocked in Russia through the proxy
domain(geosite:category-media-ru-blocked)->proxy`,
  },
  {
    key: "ruServices",
    region: true,
    code: `# Russian services direct
domain(geosite:category-ru, geosite:category-bank-ru, geosite:category-gov-ru)->direct`,
  },
  {
    key: "uaIps",
    region: true,
    code: `# Ukraine IPs direct
ip(geoip:ua)->direct`,
  },
  {
    key: "byIps",
    region: true,
    code: `# Belarus IPs direct
ip(geoip:by)->direct`,
  },
  {
    key: "kzIps",
    region: true,
    code: `# Kazakhstan IPs direct
ip(geoip:kz)->direct`,
  },
  {
    key: "irPreset",
    region: true,
    code: `# Iranian sites direct
domain(geosite:category-ir)->direct
ip(geoip:ir)->direct`,
  },
  {
    key: "trIps",
    region: true,
    code: `# Türkiye IPs direct
ip(geoip:tr)->direct`,
  },
  { key: "ads", code: "domain(geosite:category-ads-all)->block" },
  {
    key: "streaming",
    code: "domain(geosite:netflix, geosite:disney, geosite:hbo, geosite:primevideo, geosite:youtube, geosite:spotify, geosite:tiktok)->proxy",
  },
  {
    key: "social",
    code: "domain(geosite:category-social-media-!cn, geosite:telegram, geosite:discord, geosite:reddit)->proxy",
  },
  {
    key: "telegram",
    code: `domain(geosite:telegram)->proxy
${telegramRanges}`,
  },
  { key: "ai", code: "domain(geosite:category-ai-!cn)->proxy" },
  {
    key: "dev",
    code: "domain(geosite:github, geosite:gitlab, geosite:docker, geosite:npmjs, geosite:jetbrains, geosite:huggingface)->proxy",
  },
  {
    key: "cnServices",
    region: true,
    code: "domain(geosite:apple@cn, geosite:google@cn, geosite:microsoft@cn, geosite:steam@cn, geosite:category-games@cn, geosite:bilibili)->direct",
  },
  {
    key: "appleMicrosoft",
    code: "domain(geosite:apple, geosite:microsoft)->direct",
  },
  { key: "games", code: "domain(geosite:category-games)->direct" },
  { key: "speedtest", code: "domain(geosite:speedtest)->direct" },
  {
    key: "lan",
    code: `domain(geosite:private)->direct
ip(geoip:private)->direct`,
  },
  { key: "bittorrent", code: "protocol(bittorrent)->direct" },
  { key: "quic", code: "port(443) && network(udp)->block" },
];
