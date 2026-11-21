# Proxy transparente

Com o proxy transparente ativado, o tráfego chega ao núcleo sem nenhuma configuração nos aplicativos; o tráfego abrangido depende da implementação. **Configurações → Proxy transparente/Proxy do sistema** ativa o recurso e seleciona a política de divisão; a configuração abaixo seleciona a implementação.

## Políticas

| Política                    | Tráfego pelo proxy                                                                                                                                                                                                    |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Não dividir                 | tudo                                                                                                                                                                                                                  |
| Proxy exceto allowlist     | tudo, exceto domínios e endereços do allowlist (`geosite:cn`, `geoip:cn`) e endereços privados; sites fora do allowlist, Google e endereços de Hong Kong e Macau passam pelo proxy mesmo quando uma regra do allowlist corresponde |
| Proxy apenas para blocklist | os domínios do conjunto blocklist (`gfw` e `greatfire` do arquivo de Loyalsoldier) e as faixas de endereços do Telegram; execute **Atualizar dados de blocklist** primeiro, pois o modo é recusado sem o arquivo |
| Igual ao da porta de regras | o modo da porta de regras, incluindo RoutingA                                                                                                                                                                         |

## Implementações

| Implementação    | Plataformas                                                        | Observações                                                                                                              |
| ---------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `redirect`       | Linux                                                              | `REDIRECT` do iptables/nftables; apenas TCP, além de DNS na porta 53 redirecionado ao módulo DNS do núcleo (porta 52353) no modo *serviço e interceptação* |
| `tproxy`         | Linux                                                              | `TPROXY` do iptables/nftables; TCP e UDP                                                                                 |
| `tun`            | Linux, Windows, macOS                                              | o núcleo abre um dispositivo TUN; TCP e UDP; exclui o v2rayA e o próprio núcleo automaticamente                          |
| Proxy do sistema | Windows; Linux e macOS quando não é executado como root (`--lite`) | configura o proxy do ambiente gráfico (GNOME e KDE no Linux); abrange apenas os aplicativos que as respeitam             |
| Por aplicativo | Linux | um programa cgroup eBPF compara o programa que abre o socket com as regras `process(...)` do RoutingA; TCP sobre IPv4 e IPv6 e UDP sobre IPv4; o DHCP fica intocado |

`redirect` e `tproxy` exigem root e `iptables` ou `nftables`; `tun` exige `/dev/net/tun` e `ip` no Linux e privilégios de administrador no Windows e no macOS.

**Prefixos de interfaces excluídos** mantém o tráfego que chega pelas interfaces indicadas (bridges do Docker, túneis VPN; `docker*`, `veth*`, `wg*`, `ppp*` por padrão) fora de `redirect` e `tproxy`; o DNS dessas interfaces só é interceptado no modo *serviço e interceptação*.

`--redirect-respect-bound-device` (Linux) deixa as conexões TCP vinculadas a uma interface com `SO_BINDTODEVICE` fora do `redirect`: as verificações de conectividade do NetworkManager são conexões desse tipo e, redirecionadas, informam uma conexão limitada e mantêm os aplicativos offline. Desligado por padrão. Quando ligado, o serviço marca esses sockets com `0x80` por cgroup BPF; isso requer kernel 5.14 ou mais recente e cgroup v2, e sem eles o `redirect` não inicia.

## Por aplicativo

`app_router` decide por programa, não por endereço. Um programa cgroup lê o executável por trás do socket, compara-o com as regras `process(...)` do editor RoutingA e deixa a conexão intocada, entrega-a ao outbound que a regra nomeia ou a recusa.

```
process(firefox) -> proxy
process(curl) && port(443) -> work
process(steam) && network(udp) -> proxy
process(transmission) -> block
process(ssh) && domain(private.example.com) -> direct
```

Um valor sem parâmetro é o nome do arquivo; `name:`, `path:` e `folder:` dizem de que parte do programa se trata, e `*` e `?` são curingas. As regras são lidas de cima para baixo e a primeira que casa vence. `network(tcp)` ou `network(udp)` restringe a regra a um transporte; sem isso a regra cobre os dois. Um literal IPv6 em `ip(...)` vai entre aspas, como `ip("2001:db8::/32")` ou `ip("::1-::5")`; uma regra que nomeia uma família não casa com a outra. Uma regra pode selecionar pelo nome de onde a conexão foi resolvida: `domain(example.com)` cobre o nome e os subdomínios, e `full:`, `suffix:`, `keyword:` e `regexp:` dizem exatamente o que comparar. O nome não está no pacote, então vem do que o módulo de DNS respondeu: o modo precisa do módulo de DNS em modo *hijack*, o módulo informa cada par nome-endereço ao serviço, e o serviço os guarda pelo TTL do endereço. Um programa que resolve nomes em outro lugar — o próprio resolvedor DoH, um servidor fixo — não é reconhecido assim, nem uma conexão feita a um endereço antes de o nome ser perguntado. Um programa com regra de domínio é julgado pelo serviço em cada conexão, porque o núcleo não consegue avaliar um nome. Um programa que nenhuma regra nomeia não é tocado, e uma regra `process(...)` fica inativa enquanto outra implementação estiver escolhida — o log diz quantas foram ignoradas.

O modo exige root, kernel 5.14 ou mais recente e cgroup v2 em `/sys/fs/cgroup`. Ele carrega TCP sobre IPv4 e IPv6, e UDP sobre IPv4 — incluindo um destino IPv4 alcançado por um socket IPv6. Um datagrama IPv6 nativo é recusado em vez de passar ao lado da sua regra, porque o núcleo não leva um datagrama reescrito para o relay IPv4. Um programa é aprendido por uma conexão que o serviço vê, então um que só envie datagramas IPv6 nativos é recusado até fazer uma. Os datagramas são comparados por destino, então um socket que fala com vários lugares recebe a regra certa para cada datagrama. O DHCP nunca é proxiado. O programa por trás do socket é identificado por `/proc`, então o serviço precisa enxergar o espaço de processos do host: num contêiner com namespace de PID próprio os programas não são identificados e suas conexões ficam intocadas.

A primeira conexão de um programa — ou o primeiro datagrama, para quem usa UDP — é julgada pelo serviço, que então guarda a resposta no núcleo; um programa bloqueado é recusado pelo núcleo já a partir da segunda conexão ou datagrama. Um programa cujas regras não cabem no núcleo passa a ser julgado pelo serviço em cada fluxo, e o registro o nomeia.

Uma conexão cujo programa não pode ser identificado — o processo terminou antes da consulta, ou o serviço não o enxerga — segue **Proprietário desconhecido**: por padrão fica intocada, e pode ser configurada para ser recusada, para que uma regra de bloqueio não falhe aberta.

## TUN

O núcleo cria o dispositivo TUN e atribui seu endereço. Com **Rota automática** ativada, o v2rayA instala as rotas e, no modo *serviço e interceptação*, direciona o resolvedor do sistema para o núcleo; com ela desativada, os scripts de configuração e remoção em **Configurar script de rota** fazem isso.

Nunca entram no TUN: as conexões do próprio núcleo, a saída `direct` e as consultas do módulo DNS aos servidores upstream (por marca de socket no Linux, por vinculação à interface física no Windows e no macOS). O v2rayA e o núcleo são sempre excluídos; **Processos excluídos do TUN** exclui outros pelo nome do executável, um por linha, para conexões cujo processo responsável pode ser identificado; no modo *serviço e interceptação*, o DNS desses processos ainda vai para o módulo DNS do núcleo. Rotas mais específicas que a padrão (redes conectadas, rotas estáticas) também não passam pelo TUN.

No modo *serviço e interceptação*, o DNS sem criptografia na porta 53 que chega ao TUN é respondido pelo módulo DNS do núcleo de acordo com as regras de DNS; o DNS criptografado não é interceptado. Nesse caso, no Windows, o resolvedor do sistema é direcionado para o gateway do TUN; no macOS, para o serviço do núcleo que escuta em `127.0.0.1`, que precisa da porta 53 livre.

Nos outros dois modos o TUN não monta esse relay e o resolvedor do sistema não é alterado em nenhuma das duas plataformas; uma consulta que ainda chegar sai direto do dispositivo. O mesmo vale com a **Rota automática** desligada e o script de rota no comando da rede: o v2rayA não muda a configuração do resolvedor, então o DNS também cabe ao script.

Limitação conhecida: no Windows e no macOS, um aplicativo que consulta diretamente um resolvedor da rede local ainda não passa pelo TUN.

## DNS

**Configurações → Configurações de DNS** contém o modo do DNS e as regras que o módulo DNS do núcleo segue.

| Modo                  | Módulo DNS | Consultas do sistema                                                       |
| --------------------- | ---------- | ------------------------------------------------------------------------- |
| Desligado             | não roda   | não são alteradas                                                          |
| Somente serviço       | roda       | não são alteradas: o resolvedor, as regras de DNS do firewall e o relay da TUN ficam como estão |
| Serviço e interceptação | roda      | são direcionadas a ele: o resolvedor é reapontado e a porta 53 é desviada   |

O modo responde a duas perguntas que antes se confundiam: se o módulo roda e se as consultas do sistema vão para ele — e o modo *somente serviço* responde de forma diferente às duas. *Desligado* e *somente serviço* deixam o TCP/UDP simples na porta 53 passar em `redirect`, `tproxy` e no proxy do sistema, e o `tun` o envia direto para fora; o DNS criptografado nunca vai para o módulo, então não há motivo para abdicar da interceptação por causa dele. Escolha *somente serviço* quando outro programa já aponta o DNS para o v2rayA, e *desligado* quando nada deve responder DNS por você.

**Configurações → Configurações de DNS** contém as regras que o módulo DNS do núcleo segue: qual servidor upstream responde por quais domínios e se a consulta sai diretamente. Os padrões enviam nomes privados para `127.0.0.1:53` (`localhost`, que precisa de um resolvedor escutando ali) e todo o resto para `1.0.0.1` pelo proxy. Uma saída `direct` consulta diretamente; qualquer outro valor envia a consulta pela entrada SOCKS local, de modo que ela é roteada como tráfego SOCKS. A regra com uma lista de domínios vazia responde por todos os domínios que nenhuma outra regra indica. Um servidor upstream é um endereço (`8.8.8.8`, `dns.google`), `tcp://host`, `tls://host` para DNS sobre TLS ou `https://host/dns-query` para DNS sobre HTTPS; DNS sobre QUIC não é suportado e é recusado ao salvar.

*Desligado* esconde as regras e as mantém: nada as leria de qualquer forma, e apagá-las faria com que religar o modousse uma configuração diferente.

Salvar grava as regras e o modo em duas requisições, e a caixa de diálogo informa qual delas o serviço aceitou. Uma recusa depois de as regras terem sido gravadas é dita como tal, para que a nova tentativa envie apenas o que falta.

## Compartilhamento com a rede local

**Compartilhamento de portas** faz as entradas SOCKS, HTTP e personalizadas escutarem em todas as interfaces em vez de `127.0.0.1`, para que outros dispositivos possam usar esta máquina como proxy; a porta da API permanece no loopback. Para rotear o tráfego desses dispositivos também pelo proxy transparente, use `redirect`, `tproxy` ou `tun`, ative **Encaminhamento de IP**, mantenha a interface pela qual o tráfego chega fora dos prefixos excluídos e direcione o gateway padrão dos dispositivos para esta máquina; os modos de proxy do sistema não podem fazer isso.
