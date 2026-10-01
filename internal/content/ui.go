package content

// UI holds the fixed interface strings of one Locale.
type UI struct {
	Skip, Mirror, Language, Menu, Breadcrumb, Specs, Materials, FAQ, Privacy, BackHome string
	NotFoundTitle, NotFoundText, Home, Property, Value, Source                         string
	// BarLong and BarShort say on every page that this is a test site; BarLink goes to tribelt.nl.
	BarLong, BarShort, BarLink                  string
	SignIn, Stats, OpenMenu, CloseMenu, Utility string
	HelpTitle, HelpLink                         string
	ContactCol, SiteCol, Attribution            string
}

func uiStrings(locale string) UI {
	switch locale {
	case "en":
		return UI{
			Skip: "Skip to content", Mirror: "student test site", Language: "Language", Menu: "Main menu", Breadcrumb: "Breadcrumb",
			Specs: "Specifications", Materials: "Materials", FAQ: "Frequently asked questions", Privacy: "Privacy",
			BackHome: "Back to the home page", NotFoundTitle: "Page not found",
			NotFoundText: "This page does not exist on the Tribelt mirror. The home page lists every product and sector.",
			Home:         "Home", Property: "Property", Value: "Value", Source: "Adapted from the official page",
			BarLong:  "Student project: this is a test site, not Tribelt's official website.",
			BarShort: "Test site, not Tribelt's official website.",
			BarLink:  "Go to tribelt.nl",
			SignIn:   "Sign in", Stats: "Stats", OpenMenu: "Menu", CloseMenu: "Close menu", Utility: "More",
			HelpTitle: "How can we help you?", HelpLink: "Contact Tribelt",
			ContactCol: "Contact", SiteCol: "This test site", Attribution: "Content and photos: Tribelt B.V., used with permission",
		}
	case "de":
		return UI{
			Skip: "Zum Inhalt springen", Mirror: "studentische Testseite", Language: "Sprache", Menu: "Hauptmenü", Breadcrumb: "Brotkrumen",
			Specs: "Technische Daten", Materials: "Werkstoffe", FAQ: "Häufig gestellte Fragen", Privacy: "Datenschutz",
			BackHome: "Zur Startseite", NotFoundTitle: "Seite nicht gefunden",
			NotFoundText: "Diese Seite gibt es auf dem Tribelt-Mirror nicht. Die Startseite führt zu allen Produkten und Branchen.",
			Home:         "Start", Property: "Eigenschaft", Value: "Wert", Source: "Bearbeitet nach der offiziellen Seite",
			BarLong:  "Studienprojekt: Dies ist eine Testseite, nicht die offizielle Website von Tribelt.",
			BarShort: "Testseite, nicht die offizielle Tribelt-Website.",
			BarLink:  "Zu tribelt.nl",
			SignIn:   "Anmelden", Stats: "Statistiken", OpenMenu: "Menü", CloseMenu: "Menü schließen", Utility: "Mehr",
			HelpTitle: "Wie können wir Ihnen helfen?", HelpLink: "Tribelt kontaktieren",
			ContactCol: "Kontakt", SiteCol: "Diese Testseite", Attribution: "Inhalte und Fotos: Tribelt B.V., mit Erlaubnis verwendet",
		}
	default:
		return UI{
			Skip: "Naar de inhoud", Mirror: "studententestsite", Language: "Taal", Menu: "Hoofdmenu", Breadcrumb: "Kruimelpad",
			Specs: "Specificaties", Materials: "Materialen", FAQ: "Veelgestelde vragen", Privacy: "Privacy",
			BackHome: "Terug naar de homepage", NotFoundTitle: "Pagina niet gevonden",
			NotFoundText: "Deze pagina bestaat niet op de Tribelt-mirror. Op de homepage vindt u alle producten en sectoren.",
			Home:         "Home", Property: "Eigenschap", Value: "Waarde", Source: "Bewerkt naar de officiële pagina",
			BarLong:  "Studieproject: dit is een testsite, niet de officiële website van Tribelt.",
			BarShort: "Testsite, niet de officiële website van Tribelt.",
			BarLink:  "Naar tribelt.nl",
			SignIn:   "Inloggen", Stats: "Statistieken", OpenMenu: "Menu", CloseMenu: "Menu sluiten", Utility: "Meer",
			HelpTitle: "Waarmee kunnen we u helpen?", HelpLink: "Contact opnemen met Tribelt",
			ContactCol: "Contact", SiteCol: "Deze testsite", Attribution: "Inhoud en foto's: Tribelt B.V., gebruikt met toestemming",
		}
	}
}

func ogLocale(locale string) string {
	switch locale {
	case "en":
		return "en_GB"
	case "de":
		return "de_DE"
	default:
		return "nl_NL"
	}
}
