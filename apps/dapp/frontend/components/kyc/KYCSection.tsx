"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    ShieldCheck,
    Clock,
    CheckCircle2,
    XCircle,
    Upload,
    AlertCircle,
    ChevronRight,
    FileText,
    User,
    Globe,
    Calendar,
    CreditCard,
} from "lucide-react";
import { cn } from "@/lib/utils";

export type KYCStatus = "unverified" | "pending" | "verified" | "rejected";

interface KYCStatusBadgeProps {
    status: KYCStatus;
}

export function KYCStatusBadge({ status }: KYCStatusBadgeProps) {
    const config = {
        unverified: {
            label: "Unverified",
            className: "bg-gray-100 text-gray-600",
            Icon: AlertCircle,
        },
        pending: {
            label: "Pending Review",
            className: "bg-amber-50 text-amber-700",
            Icon: Clock,
        },
        verified: {
            label: "Verified",
            className: "bg-emerald-50 text-emerald-700",
            Icon: CheckCircle2,
        },
        rejected: {
            label: "Rejected",
            className: "bg-red-50 text-red-700",
            Icon: XCircle,
        },
    }[status];

    const { label, className, Icon } = config;

    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium",
                className
            )}
        >
            <Icon className="h-3.5 w-3.5" />
            {label}
        </span>
    );
}

type Step = "personal" | "document" | "confirm";

interface KYCFormData {
    full_name: string;
    date_of_birth: string;
    country: string;
    id_type: string;
    id_number: string;
    id_front: File | null;
    id_back: File | null;
}

interface KYCFormProps {
    onSubmit: (data: FormData) => Promise<void>;
    isSubmitting?: boolean;
}

const ID_TYPES = [
    { value: "passport", label: "Passport" },
    { value: "national_id", label: "National ID" },
    { value: "drivers_license", label: "Driver's License" },
];

const COUNTRIES = [
    "Nigeria", "Ghana", "Kenya", "South Africa", "United States",
    "United Kingdom", "Canada", "Australia", "Germany", "France",
];

function StepIndicator({ current, steps }: { current: Step; steps: { id: Step; label: string }[] }) {
    const currentIdx = steps.findIndex((s) => s.id === current);
    return (
        <div className="flex items-center gap-2 mb-8">
            {steps.map((step, i) => {
                const done = i < currentIdx;
                const active = i === currentIdx;
                return (
                    <div key={step.id} className="flex items-center gap-2">
                        <div
                            className={cn(
                                "flex h-7 w-7 items-center justify-center rounded-full text-xs font-medium transition-colors",
                                done
                                    ? "bg-black text-white"
                                    : active
                                        ? "bg-black text-white"
                                        : "bg-black/8 text-black/35"
                            )}
                        >
                            {done ? <CheckCircle2 className="h-4 w-4" /> : i + 1}
                        </div>
                        <span className={cn("text-xs hidden sm:block", active ? "text-black" : "text-black/40")}>
                            {step.label}
                        </span>
                        {i < steps.length - 1 && (
                            <ChevronRight className="h-3.5 w-3.5 text-black/20 ml-1" />
                        )}
                    </div>
                );
            })}
        </div>
    );
}

function FileUpload({
    label,
    hint,
    onChange,
    value,
    required,
}: {
    label: string;
    hint?: string;
    onChange: (file: File | null) => void;
    value: File | null;
    required?: boolean;
}) {
    const [dragOver, setDragOver] = useState(false);

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        onChange(e.target.files?.[0] ?? null);
    };

    return (
        <div>
            <label className="mb-1.5 block text-xs text-black/45">
                {label} {required && <span className="text-red-500">*</span>}
            </label>
            <label
                className={cn(
                    "flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed py-6 cursor-pointer transition-colors",
                    dragOver
                        ? "border-black/30 bg-black/4"
                        : "border-black/12 hover:border-black/22 hover:bg-black/2",
                    value && "border-black/25 bg-black/3"
                )}
                onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
                onDragLeave={() => setDragOver(false)}
                onDrop={(e) => {
                    e.preventDefault();
                    setDragOver(false);
                    onChange(e.dataTransfer.files[0] ?? null);
                }}
            >
                <input type="file" accept="image/*,.pdf" className="sr-only" onChange={handleChange} />
                {value ? (
                    <>
                        <CheckCircle2 className="h-5 w-5 text-black/40" />
                        <span className="text-xs text-black/50 text-center px-3">{value.name}</span>
                    </>
                ) : (
                    <>
                        <Upload className="h-5 w-5 text-black/25" />
                        <span className="text-xs text-black/40 text-center px-3">
                            Click to upload or drag &amp; drop
                        </span>
                        {hint && <span className="text-[10px] text-black/30">{hint}</span>}
                    </>
                )}
            </label>
        </div>
    );
}

const STEPS: { id: Step; label: string }[] = [
    { id: "personal", label: "Personal Details" },
    { id: "document", label: "Document Upload" },
    { id: "confirm", label: "Confirmation" },
];

export function KYCForm({ onSubmit, isSubmitting }: KYCFormProps) {
    const [step, setStep] = useState<Step>("personal");
    const [data, setData] = useState<KYCFormData>({
        full_name: "",
        date_of_birth: "",
        country: "",
        id_type: "",
        id_number: "",
        id_front: null,
        id_back: null,
    });
    const [errors, setErrors] = useState<Partial<Record<keyof KYCFormData, string>>>({});

    const update = (key: keyof KYCFormData) => (value: string | File | null) =>
        setData((prev) => ({ ...prev, [key]: value }));

    const validatePersonal = () => {
        const errs: typeof errors = {};
        if (!data.full_name.trim()) errs.full_name = "Full name is required";
        if (!data.date_of_birth) errs.date_of_birth = "Date of birth is required";
        if (!data.country) errs.country = "Country is required";
        setErrors(errs);
        return Object.keys(errs).length === 0;
    };

    const validateDocument = () => {
        const errs: typeof errors = {};
        if (!data.id_type) errs.id_type = "ID type is required";
        if (!data.id_number.trim()) errs.id_number = "ID number is required";
        if (!data.id_front) errs.id_front = "Front of ID is required";
        setErrors(errs);
        return Object.keys(errs).length === 0;
    };

    const handleNext = () => {
        if (step === "personal" && validatePersonal()) setStep("document");
        else if (step === "document" && validateDocument()) setStep("confirm");
    };

    const handleBack = () => {
        if (step === "document") setStep("personal");
        else if (step === "confirm") setStep("document");
    };

    const handleSubmit = async () => {
        const fd = new FormData();
        fd.append("full_name", data.full_name);
        fd.append("date_of_birth", data.date_of_birth);
        fd.append("country", data.country);
        fd.append("id_type", data.id_type);
        fd.append("id_number", data.id_number);
        if (data.id_front) fd.append("id_front", data.id_front);
        if (data.id_back) fd.append("id_back", data.id_back);
        await onSubmit(fd);
    };

    return (
        <div>
            <StepIndicator current={step} steps={STEPS} />

            <AnimatePresence mode="wait">
                {step === "personal" && (
                    <motion.div
                        key="personal"
                        initial={{ opacity: 0, x: 16 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -16 }}
                        className="space-y-4"
                    >
                        <div>
                            <label className="mb-1.5 flex items-center gap-1.5 text-xs text-black/45">
                                <User className="h-3.5 w-3.5" /> Full Name *
                            </label>
                            <input
                                type="text"
                                value={data.full_name}
                                onChange={(e) => update("full_name")(e.target.value)}
                                placeholder="As it appears on your ID"
                                className={cn(
                                    "h-11 w-full rounded-xl border bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:bg-white",
                                    errors.full_name ? "border-red-400 focus:border-red-400" : "border-black/10 focus:border-black/25"
                                )}
                            />
                            {errors.full_name && <p className="mt-1 text-[11px] text-red-500">{errors.full_name}</p>}
                        </div>

                        <div>
                            <label className="mb-1.5 flex items-center gap-1.5 text-xs text-black/45">
                                <Calendar className="h-3.5 w-3.5" /> Date of Birth *
                            </label>
                            <input
                                type="date"
                                value={data.date_of_birth}
                                onChange={(e) => update("date_of_birth")(e.target.value)}
                                className={cn(
                                    "h-11 w-full rounded-xl border bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:bg-white",
                                    errors.date_of_birth ? "border-red-400" : "border-black/10 focus:border-black/25"
                                )}
                            />
                            {errors.date_of_birth && <p className="mt-1 text-[11px] text-red-500">{errors.date_of_birth}</p>}
                        </div>

                        <div>
                            <label className="mb-1.5 flex items-center gap-1.5 text-xs text-black/45">
                                <Globe className="h-3.5 w-3.5" /> Country *
                            </label>
                            <select
                                value={data.country}
                                onChange={(e) => update("country")(e.target.value)}
                                className={cn(
                                    "h-11 w-full rounded-xl border bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:bg-white appearance-none",
                                    errors.country ? "border-red-400" : "border-black/10 focus:border-black/25",
                                    !data.country && "text-black/35"
                                )}
                            >
                                <option value="">Select your country</option>
                                {COUNTRIES.map((c) => <option key={c} value={c}>{c}</option>)}
                            </select>
                            {errors.country && <p className="mt-1 text-[11px] text-red-500">{errors.country}</p>}
                        </div>
                    </motion.div>
                )}

                {step === "document" && (
                    <motion.div
                        key="document"
                        initial={{ opacity: 0, x: 16 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -16 }}
                        className="space-y-4"
                    >
                        <div>
                            <label className="mb-1.5 flex items-center gap-1.5 text-xs text-black/45">
                                <FileText className="h-3.5 w-3.5" /> ID Type *
                            </label>
                            <select
                                value={data.id_type}
                                onChange={(e) => update("id_type")(e.target.value)}
                                className={cn(
                                    "h-11 w-full rounded-xl border bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:bg-white appearance-none",
                                    errors.id_type ? "border-red-400" : "border-black/10 focus:border-black/25",
                                    !data.id_type && "text-black/35"
                                )}
                            >
                                <option value="">Select ID type</option>
                                {ID_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                            </select>
                            {errors.id_type && <p className="mt-1 text-[11px] text-red-500">{errors.id_type}</p>}
                        </div>

                        <div>
                            <label className="mb-1.5 flex items-center gap-1.5 text-xs text-black/45">
                                <CreditCard className="h-3.5 w-3.5" /> ID Number *
                            </label>
                            <input
                                type="text"
                                value={data.id_number}
                                onChange={(e) => update("id_number")(e.target.value)}
                                placeholder="Enter your ID number"
                                className={cn(
                                    "h-11 w-full rounded-xl border bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:bg-white",
                                    errors.id_number ? "border-red-400" : "border-black/10 focus:border-black/25"
                                )}
                            />
                            {errors.id_number && <p className="mt-1 text-[11px] text-red-500">{errors.id_number}</p>}
                        </div>

                        <FileUpload
                            label="Front of ID"
                            hint="JPG, PNG or PDF · max 10 MB"
                            value={data.id_front}
                            onChange={(f) => update("id_front")(f)}
                            required
                        />
                        {errors.id_front && <p className="text-[11px] text-red-500">{errors.id_front}</p>}

                        <FileUpload
                            label="Back of ID (optional)"
                            hint="Required for national ID and driver's license"
                            value={data.id_back}
                            onChange={(f) => update("id_back")(f)}
                        />
                    </motion.div>
                )}

                {step === "confirm" && (
                    <motion.div
                        key="confirm"
                        initial={{ opacity: 0, x: 16 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -16 }}
                        className="space-y-3"
                    >
                        <div className="rounded-2xl border border-black/8 bg-black/[0.015] p-5 space-y-3">
                            {[
                                { label: "Full Name", value: data.full_name },
                                { label: "Date of Birth", value: data.date_of_birth },
                                { label: "Country", value: data.country },
                                { label: "ID Type", value: ID_TYPES.find((t) => t.value === data.id_type)?.label ?? data.id_type },
                                { label: "ID Number", value: data.id_number },
                                { label: "Front Document", value: data.id_front?.name ?? "—" },
                                { label: "Back Document", value: data.id_back?.name ?? "Not provided" },
                            ].map(({ label, value }) => (
                                <div key={label} className="flex justify-between gap-4 text-sm">
                                    <span className="text-black/40 shrink-0">{label}</span>
                                    <span className="text-black text-right truncate">{value}</span>
                                </div>
                            ))}
                        </div>
                        <p className="text-xs text-black/40 leading-relaxed">
                            By submitting, you confirm that all details are accurate. Your documents
                            will be stored securely and reviewed within 1-3 business days.
                        </p>
                    </motion.div>
                )}
            </AnimatePresence>

            <div className="mt-8 flex gap-3">
                {step !== "personal" && (
                    <button
                        onClick={handleBack}
                        disabled={isSubmitting}
                        className="flex-1 rounded-xl border border-black/12 py-3 text-sm text-black/60 hover:text-black transition-colors disabled:opacity-35"
                    >
                        Back
                    </button>
                )}
                <button
                    onClick={step === "confirm" ? handleSubmit : handleNext}
                    disabled={isSubmitting}
                    className="flex-1 rounded-xl bg-black py-3 text-sm text-white transition-opacity hover:opacity-75 disabled:opacity-35"
                >
                    {isSubmitting ? "Submitting…" : step === "confirm" ? "Submit for Review" : "Continue"}
                </button>
            </div>
        </div>
    );
}

// ── KYC Section for Settings Page ─────────────────────────────────────────────

interface KYCSectionProps {
    status: KYCStatus;
    submittedAt?: string | null;
    reviewedAt?: string | null;
    rejectionReason?: string | null;
    onSubmit: (data: FormData) => Promise<void>;
    isSubmitting?: boolean;
}

export function KYCSection({
    status,
    submittedAt,
    reviewedAt,
    rejectionReason,
    onSubmit,
    isSubmitting,
}: KYCSectionProps) {
    const [showForm, setShowForm] = useState(false);

    const handleSubmit = async (fd: FormData) => {
        await onSubmit(fd);
        setShowForm(false);
    };

    return (
        <div className="space-y-5">
            {/* Status banner */}
            <div className="flex items-center justify-between gap-4 flex-wrap">
                <div>
                    <p className="text-sm text-black">Identity Verification (KYC)</p>
                    <p className="text-xs text-black/40 mt-0.5">
                        Required to access fiat offramp and withdrawals
                    </p>
                </div>
                <KYCStatusBadge status={status} />
            </div>

            {/* Status details */}
            {status === "verified" && (
                <div className="flex items-center gap-3 rounded-2xl border border-emerald-100 bg-emerald-50/60 px-4 py-3">
                    <ShieldCheck className="h-4 w-4 text-emerald-600 shrink-0" />
                    <div>
                        <p className="text-sm text-emerald-800">Your identity has been verified</p>
                        {reviewedAt && (
                            <p className="text-xs text-emerald-600/70 mt-0.5">
                                Verified on {new Date(reviewedAt).toLocaleDateString()}
                            </p>
                        )}
                    </div>
                </div>
            )}

            {status === "pending" && (
                <div className="flex items-center gap-3 rounded-2xl border border-amber-100 bg-amber-50/60 px-4 py-3">
                    <Clock className="h-4 w-4 text-amber-600 shrink-0" />
                    <div>
                        <p className="text-sm text-amber-800">Under review — this takes 1–3 business days</p>
                        {submittedAt && (
                            <p className="text-xs text-amber-600/70 mt-0.5">
                                Submitted {new Date(submittedAt).toLocaleDateString()}
                            </p>
                        )}
                    </div>
                </div>
            )}

            {status === "rejected" && (
                <div className="space-y-3">
                    <div className="flex items-start gap-3 rounded-2xl border border-red-100 bg-red-50/60 px-4 py-3">
                        <XCircle className="h-4 w-4 text-red-500 shrink-0 mt-0.5" />
                        <div>
                            <p className="text-sm text-red-700">Verification was unsuccessful</p>
                            {rejectionReason && (
                                <p className="text-xs text-red-500/80 mt-1 leading-relaxed">
                                    Reason: {rejectionReason}
                                </p>
                            )}
                        </div>
                    </div>
                    {!showForm && (
                        <button
                            onClick={() => setShowForm(true)}
                            className="w-full rounded-xl border border-black/10 py-2.5 text-sm text-black/60 hover:text-black hover:border-black/20 transition-colors"
                        >
                            Resubmit Verification
                        </button>
                    )}
                </div>
            )}

            {status === "unverified" && !showForm && (
                <button
                    onClick={() => setShowForm(true)}
                    className="w-full rounded-xl bg-black py-3 text-sm text-white transition-opacity hover:opacity-75"
                >
                    Start Verification
                </button>
            )}

            <AnimatePresence>
                {showForm && (status === "unverified" || status === "rejected") && (
                    <motion.div
                        key="kyc-form"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        className="overflow-hidden"
                    >
                        <div className="pt-2 border-t border-black/6">
                            <KYCForm onSubmit={handleSubmit} isSubmitting={isSubmitting} />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
