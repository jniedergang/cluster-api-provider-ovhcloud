# Guide de creation des credentials API OVH pour CAPIOVH

Ce guide est destine a l'administrateur OVH qui doit creer un acces API
securise et limite pour le provider CAPI OVH Cloud.

## Pre-requis

- Un compte OVH avec acces au Manager
- Un projet Public Cloud (existant ou a creer)

## Etape 1 : Creer un projet Public Cloud isole

1. Se connecter sur https://www.ovh.com/manager/
2. Aller dans **Public Cloud** > **Creer un nouveau projet**
3. Nommer le projet **"CAPI-test"** (ou un nom descriptif)
4. Noter le **serviceName** (ID du projet) visible dans l'URL :
   `https://www.ovh.com/manager/#/public-cloud/pci/projects/<SERVICE_NAME>/`

> Le serviceName est une chaine hexadecimale, ex: `abc123def456789...`

## Etape 2 : Creer une Application Key

1. Aller sur https://eu.api.ovh.com/createApp
   - Pour les comptes OVH Canada : https://ca.api.ovh.com/createApp
2. Se connecter avec le compte OVH
3. Remplir :
   - **Application name** : `capiovh`
   - **Application description** : `Cluster API Provider OVH Cloud`
4. Valider et noter les deux valeurs obtenues :
   - **Application Key** (AK)
   - **Application Secret** (AS)

> **IMPORTANT** : L'Application Secret ne sera affiche qu'une seule fois.
> Le conserver dans un endroit securise immediatement.

## Etape 3 : Creer une Consumer Key avec droits limites

Executer la commande suivante en remplacant `<APPLICATION_KEY>` et
`<SERVICE_NAME>` par les valeurs obtenues aux etapes precedentes :

```bash
curl -X POST https://eu.api.ovh.com/1.0/auth/credential \
  -H "X-Ovh-Application: <APPLICATION_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "accessRules": [
      {"method": "GET",    "path": "/me"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/*"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/instance"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/instance/*"},
      {"method": "DELETE", "path": "/cloud/project/<SERVICE_NAME>/instance/*"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/flavor"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/image"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/sshkey"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/sshkey/*"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/sshkey"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/network/private"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/network/private/*"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/network/private/*"},
      {"method": "DELETE", "path": "/cloud/project/<SERVICE_NAME>/network/private/*"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/volume"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/volume/*"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/volume/*"},
      {"method": "DELETE", "path": "/cloud/project/<SERVICE_NAME>/volume/*"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/region/*/loadbalancing/*"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/region/*/loadbalancing/*"},
      {"method": "DELETE", "path": "/cloud/project/<SERVICE_NAME>/region/*/loadbalancing/*"},
      {"method": "GET",    "path": "/cloud/project/<SERVICE_NAME>/region/*/floatingip/*"},
      {"method": "POST",   "path": "/cloud/project/<SERVICE_NAME>/region/*/floatingip/*"},
      {"method": "DELETE", "path": "/cloud/project/<SERVICE_NAME>/region/*/floatingip/*"}
    ],
    "redirection": "https://localhost/callback"
  }'
```

La reponse contient :

```json
{
  "validationUrl": "https://eu.api.ovh.com/auth/?credentialToken=...",
  "consumerKey": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "state": "pendingValidation"
}
```

### Valider la Consumer Key

1. Ouvrir la **validationUrl** dans un navigateur
2. Se connecter avec le compte OVH
3. Choisir la duree de validite : **Unlimited** (recommande pour un service)
4. Valider

### Ce que ces droits permettent

- Creer, lire et supprimer des instances compute dans ce projet uniquement
- Lister les flavors, images et cles SSH
- Gerer les reseaux prives (vRack) dans ce projet
- Gerer les load balancers (Octavia) dans ce projet
- Gerer les volumes block storage dans ce projet
- Gerer les floating IPs dans ce projet
- Lire les informations du compte (`/me`) pour valider les credentials

### Ce que ces droits NE permettent PAS

- Acceder a la facturation ou aux moyens de paiement
- Modifier les parametres du compte OVH
- Acceder aux autres projets Public Cloud
- Gerer les noms de domaine ou zones DNS
- Acceder aux services dedies (serveurs, VPS, etc.)
- Supprimer le projet lui-meme

## Etape 4 : Transmettre les credentials

Les 4 valeurs a transmettre (de maniere securisee) :

| Valeur | Description |
|--------|-------------|
| `endpoint` | `ovh-eu` (Europe) ou `ovh-ca` (Canada) |
| `applicationKey` | Obtenue a l'etape 2 |
| `applicationSecret` | Obtenue a l'etape 2 |
| `consumerKey` | Obtenue a l'etape 3 |
| `serviceName` | ID du projet Public Cloud (etape 1) |

> **SECURITE** : Ne jamais transmettre ces valeurs par email ou chat non chiffre.
> Utiliser un gestionnaire de secrets (Vault, 1Password, etc.) ou un canal securise.

## Verification rapide

Pour verifier que les credentials fonctionnent :

```bash
# Remplacer les valeurs
export OVH_ENDPOINT="ovh-eu"
export OVH_APPLICATION_KEY="<AK>"
export OVH_APPLICATION_SECRET="<AS>"
export OVH_CONSUMER_KEY="<CK>"

# Tester l'authentification
curl -s https://eu.api.ovh.com/1.0/auth/time

# Le timestamp OVH sert a calculer la signature
# Un test complet necessite le SDK (signature HMAC)
```

Pour un test complet avec le SDK Go :

```go
client, _ := ovh.NewClient("ovh-eu", appKey, appSecret, consumerKey)
me := map[string]interface{}{}
client.Get("/me", &me)
fmt.Println(me["nichandle"])  // Affiche le NIC handle OVH
```

## SSH key pitfall

CAPIOVH resolves the SSH key referenced by `OVHMachine.spec.sshKeyName`
through the **OVH native API**:

```
GET /cloud/project/{serviceName}/sshkey
```

OVH maintains **two parallel SSH key inventories** that do NOT sync:

| | OpenStack Nova keypair store | OVH native SSH key store |
|---|---|---|
| Created via | `openstack keypair create` | OVH manager UI, OR `POST /cloud/project/{sn}/sshkey` |
| Listed via | `openstack keypair list` | `GET /cloud/project/{sn}/sshkey` |
| Visible to CAPIOVH | **NO** | YES |

If you create the keypair with `openstack keypair create`, the controller
will fail with `resolving SSH key "<name>": SSH key "<name>" not found`,
even though the OpenStack CLI happily lists it.

**Always register via the OVH native API**:

```bash
# After exporting OVH_* env vars and computing the signature (see SDK Go)
curl -X POST "https://${EP_HOST}.api.ovh.com/1.0/cloud/project/${SERVICE_NAME}/sshkey" \
  -H "X-Ovh-Application: $AK" -H "X-Ovh-Consumer: $CK" \
  -H "X-Ovh-Timestamp: $TS"  -H "X-Ovh-Signature: $SIG" \
  -H "Content-Type: application/json" \
  -d '{"name":"capiovh-key","publicKey":"ssh-ed25519 AAAA... me","region":"EU-WEST-PAR"}'
```

OVH SSH keys are also **region-scoped** (the response includes a
`regions: [...]` array). Register the key in every region where you
intend to provision OVHMachines.
