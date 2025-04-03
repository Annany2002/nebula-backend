import React from "react";
import nebulaLogo from "./nebula-logo.jpeg";
import { Link } from "react-router-dom";

interface NebulaLogoProps {
  className?: string;
}

export const NebulaLogo: React.FC<NebulaLogoProps> = () => {
  return (
    <Link to={"/"}>
      <div className="flex items-center gap-2">
        {/* <img
          src={nebulaLogo}
          alt="nebula-logo"
          className="rounded-full w-12 h-12 mr-[-11px]"
        /> */}
        <span className="inline-flex gradient-text font-semibold  text-[18px]">
          nebula
        </span>
      </div>
    </Link>
  );
};

export default NebulaLogo;
