---
id: privacyverklaring
locale: nl
path: /privacyverklaring
official: https://www.tribelt.nl/privacyverklaring
type: privacy
title: "Privacy op deze testsite: cookie, statistiek en bewaring"
description: "Hoe deze studententestsite bezoeken meet: één analytische cookie (tv, max. 13 maanden), geen IP-opslag, niets delen met derden, bewaring tot projecteinde."
keywords: [privacyverklaring, analytische cookie, Telecommunicatiewet 11.7a, bezoekersstatistiek, geen IP-opslag, Global Privacy Control]
h1: "Privacyverklaring van deze testsite"
cta: { label: "Privacyverklaring van Tribelt zelf op tribelt.nl", href: "https://www.tribelt.nl/privacyverklaring" }
noindex: false
---
## Over deze verklaring

Deze privacyverklaring gaat over tribelt.jorisjonkers.dev. Dat is een testsite van een student voor een universitair project over digitale strategie. De site volgt de opbouw van tribelt.nl en is met toestemming van Tribelt gemaakt, maar is **niet** de website van Tribelt B.V. Tribelt verwerkt via deze site geen gegevens en is er niet verantwoordelijk voor.

Verantwoordelijk voor deze site is de student die de site beheert, bereikbaar via de eigenaar van [jorisjonkers.dev](https://jorisjonkers.dev). Voor de privacyverklaring van Tribelt zelf, over tribelt.nl, zie de link onderaan deze pagina.

## Waarom deze site meet

Het project onderzoekt hoe zoekmachines, AI-assistenten en crawlers inhoud vinden en gebruiken, en hoe mensen op de site terechtkomen. Daarvoor telt de site hoe vaak pagina's worden opgevraagd, door wie (mens of bot) en via welk kanaal. De uitkomsten worden alleen als totalen gebruikt, bijvoorbeeld "zoveel bezoeken via een zoekmachine per week".

De site heeft geen formulieren en vraagt nooit om naam, e-mailadres of telefoonnummer. Offertes, contact en sollicitaties lopen via tribelt.nl.

## Wat per paginabezoek wordt opgeslagen

Bij elke opvraging van een pagina of bestand (ook robots.txt, llms.txt, sitemap en Markdown-versies) slaat de site één regel op met:

- tijdstip, opgevraagde pagina, taal, formaat (HTML, Markdown, tekst of XML) en statuscode
- de inhoudsversie van de site (Content Release) op dat moment
- het soort bezoeker: mens, onbevestigd mens, zoekmachine-crawler, AI-crawler, AI-fetcher, SEO-tool of andere bot, en bij een bot de naam en of de identiteit is geverifieerd
- bij mensen het herkomstkanaal: zoekmachine, AI-chat, sociale media, campagne, verwijzing, direct of intern
- alleen de domeinnaam van de verwijzende website (geen volledige adressen), en de naam van de zoekmachine of AI-dienst als die herkenbaar is
- eventuele utm-campagneparameters uit de link
- het land, als landcode die Cloudflare meestuurt (bijvoorbeeld "NL"); geen stad of precieze locatie
- de volledige user-agent van de browser of bot, zodat bezoeken later opnieuw kunnen worden ingedeeld
- een bezoekers-ID uit de cookie, of anders een dagelijkse hash (zie hieronder)
- of een klein script bevestigde dat de pagina echt in een browser werd geopend, en hoe lang de pagina ongeveer in beeld was

Klikt u op een link naar tribelt.nl, dan wordt die klik geregistreerd (vanaf welke pagina, naar welke pagina op tribelt.nl) en wordt u direct doorgestuurd.

## Geen IP-adressen

Uw IP-adres wordt **niet opgeslagen**. Het wordt alleen kort in het werkgeheugen gebruikt voor twee dingen: controleren of een bezoeker die zich als crawler (zoals Googlebot) voordoet echt van die partij komt, door het adres te vergelijken met de gepubliceerde adresreeksen, en het berekenen van de dagelijkse hash. Daarna wordt het adres weggegooid.

## De cookie `tv`

Om terugkerende bezoekers te kunnen tellen, zet de site één eigen cookie met de naam `tv`:

- inhoud: een willekeurig getal zonder betekenis, geen persoonsgegevens
- alleen voor deze website (first-party, host-only), alleen via een beveiligde verbinding, niet leesbaar voor scripts (HttpOnly), SameSite=Lax
- bewaartijd in uw browser: maximaal 13 maanden
- gebruikt voor statistiek over deze site, niet voor advertenties of profilering, en niet gedeeld met anderen

Dit is een analytische cookie met geen of weinig gevolgen voor uw privacy. Daarvoor is geen toestemming nodig op grond van artikel 11.7a, derde lid, onder b van de Telecommunicatiewet. Daarom toont deze site geen cookiebanner.

De cookie wordt **niet** gezet als uw browser Global Privacy Control (`Sec-GPC: 1`) of Do Not Track meestuurt, en ook niet voor bots. U kunt de cookie bovendien op elk moment verwijderen via de instellingen van uw browser.

## Dagelijkse bezoekers zonder cookie

Voor bezoekers zonder cookie (bots, GPC of Do Not Track, of cookies uitgeschakeld) berekent de site een dagelijkse hash van IP-adres en user-agent. Die hash wordt gemaakt met een geheime sleutel die elke dag (Amsterdamse tijd) wordt vervangen en alleen in het werkgeheugen bestaat. Daardoor kunnen bezoeken op dezelfde dag worden gegroepeerd, maar is het niet mogelijk om dezelfde bezoeker op verschillende dagen te herkennen of de hash terug te rekenen naar een IP-adres.

## Zoekmachinegegevens

Daarnaast haalt het project geaggregeerde cijfers op uit Google Search Console en Bing Webmaster Tools: per pagina en per zoekterm het aantal vertoningen, klikken en de gemiddelde positie. Die cijfers gaan niet over individuele bezoekers.

## Beheerders

Alleen ingelogde beheerders van het project kunnen de statistieken bekijken. Voor hen zet de site een aparte, strikt noodzakelijke sessiecookie. Hun eigen bezoeken worden als "intern" gemarkeerd en standaard niet meegeteld.

## Bewaartermijn

Alle meetgegevens worden bewaard tot het einde van het project en daarna verwijderd. Gegevens die voor analyse worden geëxporteerd, zijn geanonimiseerd.

## Delen met anderen

De gegevens worden niet verkocht en niet met derden gedeeld, ook niet met Tribelt; in het projectverslag komen alleen totalen. De site gebruikt geen advertentie- of analysediensten van derden. Het verkeer loopt wel via Cloudflare, dat de site als netwerk- en beveiligingsdienst doorgeeft en daarbij de landcode aanlevert.

## Uw rechten

U heeft het recht op inzage, correctie en verwijdering van gegevens die over u zijn opgeslagen. Omdat de site geen namen, e-mailadressen of IP-adressen bewaart, kan een verzoek alleen worden gekoppeld aan de waarde van uw `tv`-cookie; stuur die mee als u een verzoek doet. Ook kunt u een klacht indienen bij de [Autoriteit Persoonsgegevens](https://autoriteitpersoonsgegevens.nl).

## Contact

Vragen over deze testsite of over deze verklaring kunt u richten aan de student die de site beheert, via de eigenaar van [jorisjonkers.dev](https://jorisjonkers.dev).

E-mail: [contact@jorisjonkers.dev](mailto:contact@jorisjonkers.dev)

Voor vragen over Tribelt, zijn producten of zijn eigen privacybeleid kunt u terecht op [tribelt.nl](https://www.tribelt.nl).
