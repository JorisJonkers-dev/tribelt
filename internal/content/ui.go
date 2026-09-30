package content

// UI holds the fixed interface strings of one Locale.
type UI struct {
	Skip, Mirror, Language, Menu, Breadcrumb, Specs, Materials, FAQ, Privacy, BackHome string
	NotFoundTitle, NotFoundText, Home, Property, Value, Source                         string
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
		}
	case "de":
		return UI{
			Skip: "Zum Inhalt springen", Mirror: "studentische Testseite", Language: "Sprache", Menu: "Hauptmenü", Breadcrumb: "Brotkrumen",
			Specs: "Technische Daten", Materials: "Werkstoffe", FAQ: "Häufig gestellte Fragen", Privacy: "Datenschutz",
			BackHome: "Zur Startseite", NotFoundTitle: "Seite nicht gefunden",
			NotFoundText: "Diese Seite gibt es auf dem Tribelt-Mirror nicht. Die Startseite führt zu allen Produkten und Branchen.",
			Home:         "Start", Property: "Eigenschaft", Value: "Wert", Source: "Bearbeitet nach der offiziellen Seite",
		}
	default:
		return UI{
			Skip: "Naar de inhoud", Mirror: "studententestsite", Language: "Taal", Menu: "Hoofdmenu", Breadcrumb: "Kruimelpad",
			Specs: "Specificaties", Materials: "Materialen", FAQ: "Veelgestelde vragen", Privacy: "Privacy",
			BackHome: "Terug naar de homepage", NotFoundTitle: "Pagina niet gevonden",
			NotFoundText: "Deze pagina bestaat niet op de Tribelt-mirror. Op de homepage vindt u alle producten en sectoren.",
			Home:         "Home", Property: "Eigenschap", Value: "Waarde", Source: "Bewerkt naar de officiële pagina",
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
