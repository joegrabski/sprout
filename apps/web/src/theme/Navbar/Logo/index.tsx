import BrowserOnly from "@docusaurus/BrowserOnly";
import Link from "@docusaurus/Link";
import { useThemeConfig } from "@docusaurus/theme-common";
import useBaseUrl from "@docusaurus/useBaseUrl";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import type { ReactNode } from "react";
import Mascot from "../../../components/Landing/Mascot";

// Swizzled: the navbar logo is the same live dot-shader mascot as the hero
// (Landing/Mascot.tsx), just displayed smaller — not a separate static
// asset. Falls back to the static SVG for the SSR/pre-hydration frame,
// since canvas needs a browser.
const NAV_MASCOT_SIZE = 27;

export default function NavbarLogo(): ReactNode {
  const {
    siteConfig: { title },
  } = useDocusaurusContext();
  const {
    navbar: { title: navbarTitle, logo },
  } = useThemeConfig();
  const logoLink = useBaseUrl(logo?.href || "/");
  const staticLogoSrc = useBaseUrl(logo?.src || "img/logo.svg");
  const fallbackAlt = navbarTitle ? "" : title;
  const alt = logo?.alt ?? fallbackAlt;

  return (
    <Link
      to={logoLink}
      className="navbar__brand"
      {...(logo?.target && { target: logo.target })}
    >
      <div className="navbar__logo">
        <BrowserOnly
          fallback={
            <img
              src={staticLogoSrc}
              alt={alt}
              width={NAV_MASCOT_SIZE}
              height={NAV_MASCOT_SIZE}
            />
          }
        >
          {() => <Mascot displayWidth={NAV_MASCOT_SIZE} interactive={false} />}
        </BrowserOnly>
      </div>
      {navbarTitle != null && (
        <b className="navbar__title text--truncate">{navbarTitle}</b>
      )}
    </Link>
  );
}
