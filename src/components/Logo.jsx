import React from 'react';

const Logo = ({ size = 64, className = "" }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 100 100"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    style={{ overflow: 'visible' }}
  >
    <g transform="translate(50 50) rotate(45) translate(-50 -50)">

      {/* Top Left Bracket - Blue */}
      <path
        transform="translate(-3 -3)"
        d="M 15 15 L 45 15 A 5 5 0 0 1 50 20 L 50 30 A 5 5 0 0 1 45 35 L 35 35 L 35 45 A 5 5 0 0 1 30 50 L 20 50 A 5 5 0 0 1 15 45 Z"
        fill="#2A85C8"
      />

      {/* Top Right Bracket - Yellow */}
      <path
        transform="translate(3 -3)"
        d="M 85 15 L 85 45 A 5 5 0 0 1 80 50 L 70 50 A 5 5 0 0 1 65 45 L 65 35 L 55 35 A 5 5 0 0 1 50 30 L 50 20 A 5 5 0 0 1 55 15 Z"
        fill="#F5BB26"
      />

      {/* Bottom Right Bracket - Gold/Olive */}
      <path
        transform="translate(3 3)"
        d="M 85 85 L 55 85 A 5 5 0 0 1 50 80 L 50 70 A 5 5 0 0 1 55 65 L 65 65 L 65 55 A 5 5 0 0 1 70 50 L 80 50 A 5 5 0 0 1 85 55 Z"
        fill="#C4B51C"
      />

      {/* Bottom Left Bracket - Green */}
      <path
        transform="translate(-3 3)"
        d="M 15 85 L 15 55 A 5 5 0 0 1 20 50 L 30 50 A 5 5 0 0 1 35 55 L 35 65 L 45 65 A 5 5 0 0 1 50 70 L 50 80 A 5 5 0 0 1 45 85 Z"
        fill="#11A94E"
      />

    </g>

    {/* Center Microchip - Dark Blue. Not rotated so it stays horizontal! */}
    <g transform="translate(50 50) translate(-50 -50)">
      {/* Chip Body */}
      <rect x="36" y="36" width="28" height="28" rx="4" fill="#0D2232" />

      {/* Top Pins */}
      <rect x="41" y="30" width="4" height="8" rx="1" fill="#0D2232" />
      <rect x="48" y="30" width="4" height="8" rx="1" fill="#0D2232" />
      <rect x="55" y="30" width="4" height="8" rx="1" fill="#0D2232" />

      {/* Bottom Pins */}
      <rect x="41" y="62" width="4" height="8" rx="1" fill="#0D2232" />
      <rect x="48" y="62" width="4" height="8" rx="1" fill="#0D2232" />
      <rect x="55" y="62" width="4" height="8" rx="1" fill="#0D2232" />

      {/* Left Pins */}
      <rect x="30" y="41" width="8" height="4" rx="1" fill="#0D2232" />
      <rect x="30" y="48" width="8" height="4" rx="1" fill="#0D2232" />
      <rect x="30" y="55" width="8" height="4" rx="1" fill="#0D2232" />

      {/* Right Pins */}
      <rect x="62" y="41" width="8" height="4" rx="1" fill="#0D2232" />
      <rect x="62" y="48" width="8" height="4" rx="1" fill="#0D2232" />
      <rect x="62" y="55" width="8" height="4" rx="1" fill="#0D2232" />

      {/* Inner Chip Core Lines */}
      <rect x="43" y="43" width="14" height="14" rx="2" fill="none" stroke="#FFFFFF" strokeWidth="2" strokeOpacity="0.8" />
    </g>
  </svg>
);

export default Logo;
