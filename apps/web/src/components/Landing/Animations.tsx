import { useEffect } from "react";
import { animate } from "animejs";

export function LandingAnimations() {
  useEffect(() => {
    const elements = Array.from(
      document.querySelectorAll<HTMLElement>("[data-anim]"),
    );

    elements.forEach((el) => {
      el.style.opacity = "0";
      el.style.transform = "translateY(18px)";
    });

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const target = entry.target as HTMLElement;
          observer.unobserve(target);
          animate(target, {
            opacity: [0, 1],
            translateY: [18, 0],
            duration: 650,
            easing: "easeOutCubic",
          });
        });
      },
      { threshold: 0.15 },
    );

    elements.forEach((el) => observer.observe(el));

    const background = document.querySelector<HTMLElement>(".pageDither");
    if (background) {
      animate(background, {
        translateX: [-8, 8],
        translateY: [6, -6],
        duration: 12000,
        direction: "alternate",
        easing: "easeInOutSine",
        loop: true,
      });
    }

    return () => observer.disconnect();
  }, []);

  return null;
}
