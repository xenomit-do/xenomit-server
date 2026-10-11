# Cloud provisioning for xenomit is optional.
# Extend this stub with providers and resources when you are ready to deploy
# to a specific cloud; no live providers or credentials are required here.

terraform {
  required_version = ">= 1.5.0"
}

variable "region" {
  description = "Cloud region for future infrastructure (e.g. us-east-1)."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Base name prefix for xenomit resources."
  type        = string
  default     = "xenomit"
}
